package blue_green_state_monitor_go

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	bgConsul "github.com/netcracker/qubership-core-lib-go-bg-state-monitor/v2/consul"
	restConsul "github.com/netcracker/qubership-core-lib-go-rest-utils/v2/consul-propertysource"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
)

type consulClient interface {
	Login() error
	SecretId() string
}

var (
	DefaultPollingWaitTime = 5 * time.Minute
	DefaultTimeoutDuration = 30 * time.Second
	DefaultRetryDelay      = 10 * time.Second
	BgStateConsulPath      = "config/%s/application/bluegreen/bgstate"
	BgStateConsulPathNew   = "bluegreen/%s/bgstate"
)

var (
 	newPublisherFunc       = NewPublisher
 	buildTokenSupplierFunc = buildTokenSupplier
	newRestConsulClient    = func(cfg restConsul.ClientConfig) consulClient {
		return restConsul.NewClient(cfg)
	}
	getConsulTokenFunc     = func() string {
		if koanf := configloader.GetKoanf(); koanf != nil {
			return koanf.String("consul.token")
		}
		return ""
	}
	consulRetryCount = 20
	consulRetryDelay = 5 * time.Second
)

var UnknownDatetime = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

type httrError struct {
	statusCode int
	cause      error
}

func (e *httrError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" cause: %s", e.cause.Error())
	}
	return fmt.Sprintf("http error: %d %s%s", e.statusCode, http.StatusText(e.statusCode), cause)
}

var log = logging.GetLogger("bg-state-publisher")

type ConsulBlueGreenStatePublisher struct {
	statePointer *atomic.Pointer[BlueGreenState]
	watcherTask  *watcherTask
}

func buildTokenSupplier(ctx context.Context, consulURL, namespace string) (func(context.Context) (string, error), error) {
	consulToken := getConsulTokenFunc()
	if consulToken != "" {
		log.DebugC(ctx, "Using provided Consul token")
		return func(context.Context) (string, error) {
			return consulToken, nil
		}, nil
	}

	varOcg := newRestConsulClient(restConsul.ClientConfig{
		Address:   consulURL,
		Namespace: namespace,
		Ctx:       ctx,
	})

	retryCount := consulRetryCount
	retryDelay := consulRetryDelay

	var err error
	for i := 0; i < retryCount; i++ {
		err = varOcg.Login()
		if err == nil {
			log.InfoC(ctx, "Consul login successful (url=%s)", consulURL)
			return func(context.Context) (string, error) {
				return varOcg.SecretId(), nil
			}, nil
		}

		log.WarnC(ctx, "Consul login failed (url=%s): %s. Retrying in %s (attempt %d/%d)",
			consulURL, err.Error(), retryDelay, i+1, retryCount)
		time.Sleep(retryDelay)
	}

	return nil, fmt.Errorf("failed to login to Consul (url=%s) after %d retries: %w", consulURL, retryCount, err)
}

func NewPublisherWithConfig(ctx context.Context, consulURL, namespace string) (*ConsulBlueGreenStatePublisher, error) {
	tokenSupplier, err := buildTokenSupplierFunc(ctx, consulURL, namespace)
	if err != nil {
		return nil, err
	}
	return newPublisherFunc(ctx, consulURL, namespace, tokenSupplier)
}

func NewPublisher(ctx context.Context, consulUrl, namespace string,
	consulTokenSupplier func(ctx context.Context) (string, error)) (*ConsulBlueGreenStatePublisher, error) {
	log.InfoC(ctx, "Starting ConsulBlueGreenStatePublisher in '%s' with fallback '%s'",
		fmt.Sprintf(BgStateConsulPathNew, namespace), fmt.Sprintf(BgStateConsulPath, namespace))
	statePointer := &atomic.Pointer[BlueGreenState]{}
	wt := &watcherTask{
		consulUrl:               consulUrl,
		namespace:               namespace,
		lastModifyIndex:         "0",
		lastModifyIndexFallback: "0",
		pollingWaitTime:         ToConsulTTL(DefaultPollingWaitTime),
		consulTokenSupplier:     consulTokenSupplier,
		httpClient:              http.DefaultClient,
		statePointer:            statePointer,
		subscribers:             make(map[context.Context]chan BlueGreenState),
		subsLock:                &sync.RWMutex{},
	}
	wc := make(chan struct{})

	go wt.run(ctx, wc)

	timeoutDuration := DefaultTimeoutDuration
	timeoutTimer := time.NewTimer(timeoutDuration)

	select {
	case <-timeoutTimer.C:
		return nil, fmt.Errorf("ConsulBlueGreenStatePublisher failed to get ready after %s", timeoutDuration.String())
	case <-wc:
		timeoutTimer.Stop()
		log.InfoC(ctx, "Started ConsulBlueGreenStatePublisher")
		return &ConsulBlueGreenStatePublisher{statePointer: statePointer, watcherTask: wt}, nil
	}
}

func (p *ConsulBlueGreenStatePublisher) Subscribe(ctx context.Context, callback func(state BlueGreenState)) {
	subChan := make(chan BlueGreenState, 1)
	id := rand.Int()
	subscriberCxt := context.WithValue(ctx, "subscriber", id)
	log.DebugC(subscriberCxt, "Subscribing callback with id: %d", id)
	go func() {
		for {
			select {
			case <-ctx.Done():
				p.watcherTask.remSubscriber(subscriberCxt)
				log.DebugC(ctx, "Subscriber with id: %d unsubscribed", id)
				return
			case s, ok := <-subChan:
				if ok {
					process(ctx, id, s, callback)
				}
			}
		}
	}()
	p.watcherTask.addSubscriber(subscriberCxt, subChan)
	log.DebugC(subscriberCxt, "Subscribed callback with id: %d", id)
}

func (p *ConsulBlueGreenStatePublisher) GetState() BlueGreenState {
	state := p.statePointer.Load()
	return *state
}

type watcherTask struct {
	consulUrl               string
	namespace               string
	lastModifyIndex         string
	lastModifyIndexFallback string
	pollingWaitTime         string
	consulTokenSupplier     func(ctx context.Context) (string, error)
	httpClient              *http.Client
	statePointer            *atomic.Pointer[BlueGreenState]
	subscribers             map[context.Context]chan BlueGreenState
	subsLock                *sync.RWMutex
}

func (w *watcherTask) addSubscriber(ctx context.Context, sub chan BlueGreenState) {
	w.subsLock.Lock()
	defer w.subsLock.Unlock()
	w.subscribers[ctx] = sub
	// notify new sub about current state
	state := w.statePointer.Load()
	if state != nil {
		sub <- *state
	}
}

func (w *watcherTask) remSubscriber(ctx context.Context) {
	w.subsLock.Lock()
	defer w.subsLock.Unlock()
	delete(w.subscribers, ctx)
}

type bgStateAndIndex struct {
	index          string
	BlueGreenState *BlueGreenState
}

func (w *watcherTask) run(ctx context.Context, wc chan struct{}) {
	var initiated bool
	defer log.InfoC(ctx, "Stopped ConsulBlueGreenStatePublisher")
	sendRequest := func(pathTemplate string, index string) (*http.Response, error) {
		path := fmt.Sprintf(pathTemplate, w.namespace)
		var uri string
		if index == "0" {
			uri = fmt.Sprintf("%s/v1/kv/%s", w.consulUrl, path)
		} else {
			uri = fmt.Sprintf("%s/v1/kv/%s?index=%s&wait=%s", w.consulUrl, path, index, w.pollingWaitTime)
		}
		log.DebugC(ctx, "Sending request to Consul: %s", uri)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			log.ErrorC(ctx, "Failed to create HTTP request. Reason: %s", err.Error())
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if w.consulTokenSupplier != nil {
			consulToken, err := w.consulTokenSupplier(ctx)
			if err != nil {
				log.ErrorC(ctx, "Failed to retrieve Consul token. Reason: %s", err.Error())
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+consulToken)
		} else {
			log.DebugC(ctx, "consulTokenSupplier not provided, sending request without authentication")
		}
		return w.httpClient.Do(req)
	}
	sendAndParse := func(pathTemplate string, index string) (*bgStateAndIndex, error) {
		response, err := sendRequest(pathTemplate, index)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		stateAndIndex, err := parseResponse(ctx, initiated, w.namespace, response)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return stateAndIndex, &httrError{statusCode: response.StatusCode, cause: err}
		}
		return stateAndIndex, err
	}
	for {
		retryDelay := 0 * time.Second // by default, we retry immediately
		select {
		case <-ctx.Done():
			return
		default:
			stateAndIndex, err := sendAndParse(BgStateConsulPathNew, w.lastModifyIndex)
			if err != nil {
				var hErr *httrError
				if errors.Is(err, context.Canceled) {
					break
				} else if errors.As(err, &hErr) &&
					(hErr.statusCode == http.StatusNotFound ||
						hErr.statusCode == http.StatusForbidden ||
						hErr.statusCode == http.StatusUnauthorized) {
					stateAndIndex, err = sendAndParse(BgStateConsulPath, w.lastModifyIndexFallback)
					if stateAndIndex != nil {
						w.lastModifyIndexFallback = stateAndIndex.index
					}
					if err != nil {
						if errors.Is(err, context.Canceled) {
							break
						}
						retryDelay = DefaultRetryDelay
						if !errors.As(err, &hErr) || hErr.statusCode != http.StatusNotFound {
							log.WarnC(ctx, "Error happened on long polling request '%s', retrying after %s. Reason: %s",
								fmt.Sprintf(BgStateConsulPath, w.namespace), retryDelay, err.Error())
							break
						}
					}
				} else {
					retryDelay = DefaultRetryDelay
					log.WarnC(ctx, "Error happened on long polling request '%s'', retrying after %s. Reason: %s",
						fmt.Sprintf(BgStateConsulPathNew, w.namespace), retryDelay, err.Error())
					break
				}
			} else {
				w.lastModifyIndex = stateAndIndex.index
			}
			if stateAndIndex != nil {
				old := w.statePointer.Swap(stateAndIndex.BlueGreenState)
				if !initiated {
					wc <- struct{}{}
					initiated = true
				}
				if !old.Equal(stateAndIndex.BlueGreenState) {
					w.notifySubscribers(ctx, stateAndIndex.BlueGreenState)
				}
			}
		}
		// schedule next long polling request
		log.DebugC(ctx, "Scheduling watcherTask with retryDelay=%s", retryDelay.String())
		time.Sleep(retryDelay)
	}
}

func parseResponse(ctx context.Context, initiated bool, namespace string, response *http.Response) (*bgStateAndIndex, error) {
	if response.StatusCode == http.StatusNotFound {
		modifyIndex, err := GetModifyIndex(response)
		if err != nil {
			return nil, err
		}
		defaultBGState := getDefaultBGState(namespace)
		var logF func(ctx context.Context, format string, args ...interface{})
		if !initiated {
			logF = log.InfoC
		} else {
			logF = log.DebugC
		}
		logF(ctx, "There is no BG state value in Consul at '%s' (modifyIndex=%s). Setting blueGreenState to the default value=%v",
			response.Request.URL, modifyIndex, defaultBGState.String())
		return &bgStateAndIndex{
			index:          modifyIndex,
			BlueGreenState: &defaultBGState,
		}, nil
	} else if response.StatusCode == http.StatusOK {
		modifyIndex, err := GetModifyIndex(response)
		if err != nil {
			return nil, err
		}
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		var kvInfoList []bgConsul.KVInfo
		if err := json.Unmarshal(bodyBytes, &kvInfoList); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
		}
		if len(kvInfoList) == 0 {
			return nil, fmt.Errorf("empty KVInfo body of response from consul")
		}
		bgStateJson, err := kvInfoList[0].Value.Unmarshal()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal KVInfo's Value field: %w", err)
		}
		var bgState bgState
		if err := json.Unmarshal([]byte(bgStateJson), &bgState); err != nil {
			return nil, fmt.Errorf("failed to unmarshal bg state: %w", err)
		}
		originNSVersion := bgState.OriginNamespace
		peerNSVersion := bgState.PeerNamespace
		originNamespace := originNSVersion.Name
		peerNamespace := peerNSVersion.Name
		var current nsVersion
		var sibling nsVersion
		if namespace == originNamespace {
			current = originNSVersion
			sibling = peerNSVersion
		} else if namespace == peerNamespace {
			current = peerNSVersion
			sibling = originNSVersion
		} else {
			return nil, fmt.Errorf("invalid BlueGreen state response or namespace parameter. "+
				"'namespace' param = '%s' does not match neither originNamespace '%s' nor peerNamespace '%s'",
				namespace, originNamespace, peerNamespace)
		}
		convertNamespaceFunc := func(nsVersion nsVersion) (*NamespaceVersion, error) {
			var v string
			if nsVersion.Version == nil {
				v = ""
			} else {
				v = *nsVersion.Version
			}
			version, err := NewVersion(v)
			if err != nil {
				return nil, err
			}
			state, err := StateFromName(nsVersion.State)
			if err != nil {
				return nil, err
			}
			return &NamespaceVersion{
				Namespace: nsVersion.Name,
				Version:   version,
				State:     state,
			}, nil
		}
		currentNamespaceVersion, err := convertNamespaceFunc(current)
		if err != nil {
			return nil, err
		}
		siblingNamespaceVersion, err := convertNamespaceFunc(sibling)
		if err != nil {
			return nil, err
		}
		updateTime, err := time.Parse("2006-01-02T15:04:05Z", bgState.UpdateTime)
		return &bgStateAndIndex{
			index: modifyIndex,
			BlueGreenState: &BlueGreenState{
				Current:    *currentNamespaceVersion,
				Sibling:    siblingNamespaceVersion,
				UpdateTime: updateTime,
			},
		}, err
	} else {
		return nil, fmt.Errorf("unexpected response: %s", response.Status)
	}
}

func getDefaultBGState(namespace string) BlueGreenState {
	return BlueGreenState{
		Current:    NamespaceVersion{Namespace: namespace, State: StateActive, Version: NewVersionMust("v1")},
		Sibling:    nil,
		UpdateTime: UnknownDatetime,
	}
}

func (w *watcherTask) notifySubscribers(ctx context.Context, state *BlueGreenState) {
	if state == nil {
		return
	}
	w.subsLock.RLock()
	defer w.subsLock.RUnlock()
	if len(w.subscribers) > 0 {
		log.InfoC(ctx, "Notifying %d subscriber(s) about new BlueGreenState: %v", len(w.subscribers), state)
		for _, s := range w.subscribers {
			s <- *state
		}
	}
}

func process(ctx context.Context, subscriberId int, state BlueGreenState, callback func(state BlueGreenState)) {
	defer func() {
		if r := recover(); r != nil {
			log.ErrorC(ctx, "Callback subscriber with id=%d failed with %v, recovered", subscriberId, r)
		}
	}()
	log.DebugC(ctx, "Calling callback subscriber with id=%d, state=%v", subscriberId, state)
	callback(state)
}
