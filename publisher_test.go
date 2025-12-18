package blue_green_state_monitor_go

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"sync"
	"testing"
	"time"
)

const (
	testOriginNamespace = "test-namespace-origin"
	testPeerNamespace   = "test-namespace-peer"
)

type testSuite struct {
	suite.Suite
	container *consulContainer
}

func setTestDocker(t *testing.T) {
	if testDockerUrl := os.Getenv("TEST_DOCKER_URL"); testDockerUrl != "" {
		err := os.Setenv("DOCKER_HOST", testDockerUrl)
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("set DOCKER_HOST to value from 'TEST_DOCKER_URL' as '%s'.", testDockerUrl)
	} else {
		t.Logf("TEST_DOCKER_URL is empty")
	}
}
func (suite *testSuite) SetupSuite() {
	ctx := context.Background()
	setTestDocker(suite.T())
	req := testcontainers.ContainerRequest{
		Image:        "hashicorp/consul:1.15.4",
		ExposedPorts: []string{"8500/tcp", "8600/udp"},
		Cmd:          []string{"agent", "-dev", "-client", "0.0.0.0"},
		WaitingFor: wait.NewHTTPStrategy("/v1/status/leader").WithPort("8500/tcp").
			WithStatusCodeMatcher(func(status int) bool { return status == 200 }),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		suite.T().Fatal(err)
	}
	mappedPort, err := container.MappedPort(ctx, "8500")
	if err != nil {
		panic(err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.container = &consulContainer{Container: container, endpoint: fmt.Sprintf("http://%s:%s", host, mappedPort.Port())}
}

func (suite *testSuite) TearDownSuite() {
	ctx := context.Background()
	if err := suite.container.Terminate(ctx); err != nil {
		suite.T().Fatal(err)
	}
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(testSuite))
}

func (suite *testSuite) TestDefaultState() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())
		ctx, terminate := context.WithCancel(context.Background())
		defer terminate()
		statePublisher, err := NewPublisher(ctx, suite.container.endpoint, testOriginNamespace, nil)
		assertions.Nil(err)
		blueGreenState := statePublisher.GetState()
		assertions.Equal(testOriginNamespace, blueGreenState.Current.Namespace)
		assertions.Equal(StateActive, blueGreenState.Current.State)
		assertions.NotNil(blueGreenState.Current.Version)
		assertions.False(blueGreenState.Current.Version.IsEmpty())
		assertions.Equal("v1", blueGreenState.Current.Version.Value)
		assertions.Equal(1, blueGreenState.Current.Version.IntValue)

		assertions.Nil(blueGreenState.Sibling)
	})
}

func (suite *testSuite) TestJson() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())
		stateJson := `{
                   "updateTime": "2023-07-07T10:00:00Z",
                   "originNamespace": {
                       "name": "%s",
                       "state": "active",
                       "version": "v1"
                   },
                   "peerNamespace": {
                       "name": "%s",
                       "state": "idle",
                       "version": null
                   }
                 }`

		client := createConsulAPIClient(suite.T(), suite.container)
		if _, err := client.KV().Put(&api.KVPair{
			Key:   fmt.Sprintf(BgStateConsulPath, testOriginNamespace),
			Value: []byte(fmt.Sprintf(stateJson, testOriginNamespace, testPeerNamespace)),
		}, nil); nil != err {
			suite.T().Fatal(err)
		}
		ctx, terminate := context.WithCancel(context.Background())
		defer terminate()
		statePublisher, err := NewPublisher(ctx, suite.container.endpoint, testOriginNamespace, nil)
		assertions.Nil(err)
		blueGreenState := statePublisher.GetState()
		assertions.Equal(testOriginNamespace, blueGreenState.Current.Namespace)
		assertions.Equal(StateActive, blueGreenState.Current.State)
		assertions.NotNil(blueGreenState.Current.Version)
		assertions.False(blueGreenState.Current.Version.IsEmpty())
		assertions.Equal("v1", blueGreenState.Current.Version.Value)
		assertions.Equal(1, blueGreenState.Current.Version.IntValue)

		assertions.NotNil(blueGreenState.Sibling)
		assertions.Equal(testPeerNamespace, blueGreenState.Sibling.Namespace)
		assertions.Equal(StateIdle, blueGreenState.Sibling.State)
		assertions.True(blueGreenState.Sibling.Version.IsEmpty())

		assertions.Equal(time.Date(2023, 7, 7, 10, 0, 0, 0, time.UTC), blueGreenState.UpdateTime)
	})
}

func (suite *testSuite) TestJsonNew() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())
		stateJson := `{
                   "updateTime": "2023-07-07T10:00:00Z",
                   "originNamespace": {
                       "name": "%s",
                       "state": "active",
                       "version": "v1"
                   },
                   "peerNamespace": {
                       "name": "%s",
                       "state": "idle",
                       "version": null
                   }
                 }`

		client := createConsulAPIClient(suite.T(), suite.container)
		if _, err := client.KV().Put(&api.KVPair{
			Key:   fmt.Sprintf(BgStateConsulPathNew, testOriginNamespace),
			Value: []byte(fmt.Sprintf(stateJson, testOriginNamespace, testPeerNamespace)),
		}, nil); nil != err {
			suite.T().Fatal(err)
		}
		ctx, terminate := context.WithCancel(context.Background())
		defer terminate()
		statePublisher, err := NewPublisher(ctx, suite.container.endpoint, testOriginNamespace, nil)
		assertions.Nil(err)
		blueGreenState := statePublisher.GetState()
		assertions.Equal(testOriginNamespace, blueGreenState.Current.Namespace)
		assertions.Equal(StateActive, blueGreenState.Current.State)
		assertions.NotNil(blueGreenState.Current.Version)
		assertions.False(blueGreenState.Current.Version.IsEmpty())
		assertions.Equal("v1", blueGreenState.Current.Version.Value)
		assertions.Equal(1, blueGreenState.Current.Version.IntValue)

		assertions.NotNil(blueGreenState.Sibling)
		assertions.Equal(testPeerNamespace, blueGreenState.Sibling.Namespace)
		assertions.Equal(StateIdle, blueGreenState.Sibling.State)
		assertions.True(blueGreenState.Sibling.Version.IsEmpty())

		assertions.Equal(time.Date(2023, 7, 7, 10, 0, 0, 0, time.UTC), blueGreenState.UpdateTime)
	})
}

func (suite *testSuite) TestStateChange() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())
		updateTimeState1Str := "2023-07-07T10:00:00Z"
		updateTimeState2Str := "2023-07-07T11:00:00Z"
		updateTimeState1, err := time.Parse("2006-01-02T15:04:05Z", updateTimeState1Str)
		assertions.Nil(err)
		updateTimeState2, err := time.Parse("2006-01-02T15:04:05Z", updateTimeState2Str)
		assertions.Nil(err)

		state1 := bgState{
			OriginNamespace: nsVersion{Name: testOriginNamespace, Version: newVersion("v1"), State: StateActive.String()},
			PeerNamespace:   nsVersion{Name: testPeerNamespace, Version: newVersion("v2"), State: StateCandidate.String()},
			UpdateTime:      updateTimeState1Str,
		}
		state2 := bgState{
			OriginNamespace: nsVersion{Name: testOriginNamespace, Version: newVersion("v1"), State: StateLegacy.String()},
			PeerNamespace:   nsVersion{Name: testPeerNamespace, Version: newVersion("v2"), State: StateActive.String()},
			UpdateTime:      updateTimeState2Str,
		}

		wg1 := &sync.WaitGroup{}
		wg1.Add(2)
		wg2 := &sync.WaitGroup{}
		wg2.Add(2)
		tests := map[string]*sync.WaitGroup{
			testOriginNamespace: wg1,
			testPeerNamespace:   wg2,
		}
		client := createConsulAPIClient(suite.T(), suite.container)
		saveBGState(suite.T(), client, []string{testOriginNamespace, testPeerNamespace}, state1)

		for ns, wg := range tests {
			namespace := ns
			waitGroup := wg
			ctx := context.Background()
			publisherCtx, cancelPublisher := context.WithCancel(ctx)
			statePublisher, err := NewPublisher(publisherCtx, suite.container.endpoint, namespace, nil)
			assertions.Nil(err)
			subscribeCtx, cancelSubscriber := context.WithCancel(context.Background())
			counter := 0
			statePublisher.Subscribe(subscribeCtx, func(state BlueGreenState) {
				counter++
				if counter == 1 {
					var expectedState BlueGreenState
					if namespace == testOriginNamespace {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateActive},
							Sibling:    &NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateCandidate},
							UpdateTime: updateTimeState1,
						}
					} else {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateCandidate},
							Sibling:    &NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateActive},
							UpdateTime: updateTimeState1,
						}
					}
					assertions.Equal(expectedState, state)
					waitGroup.Done()
				} else if counter == 2 {
					var expectedState BlueGreenState
					if namespace == testOriginNamespace {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateLegacy},
							Sibling:    &NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateActive},
							UpdateTime: updateTimeState2,
						}
					} else {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateActive},
							Sibling:    &NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateLegacy},
							UpdateTime: updateTimeState2,
						}
					}
					assertions.Equal(expectedState, state)
					cancelSubscriber()
					cancelPublisher()
					waitGroup.Done()
				}
			})
		}
		saveBGState(suite.T(), client, []string{testOriginNamespace, testPeerNamespace}, state2)
		assertions.True(waitWG(10*time.Second, wg1, wg2))
	})
}

func (suite *testSuite) TestStateChangeNew() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())
		updateTimeState1Str := "2023-07-07T10:00:00Z"
		updateTimeState2Str := "2023-07-07T11:00:00Z"
		updateTimeState1, err := time.Parse("2006-01-02T15:04:05Z", updateTimeState1Str)
		assertions.Nil(err)
		updateTimeState2, err := time.Parse("2006-01-02T15:04:05Z", updateTimeState2Str)
		assertions.Nil(err)

		state1 := bgState{
			OriginNamespace: nsVersion{Name: testOriginNamespace, Version: newVersion("v1"), State: StateActive.String()},
			PeerNamespace:   nsVersion{Name: testPeerNamespace, Version: newVersion("v2"), State: StateCandidate.String()},
			UpdateTime:      updateTimeState1Str,
		}
		state2 := bgState{
			OriginNamespace: nsVersion{Name: testOriginNamespace, Version: newVersion("v1"), State: StateLegacy.String()},
			PeerNamespace:   nsVersion{Name: testPeerNamespace, Version: newVersion("v2"), State: StateActive.String()},
			UpdateTime:      updateTimeState2Str,
		}

		wg1 := &sync.WaitGroup{}
		wg1.Add(2)
		wg2 := &sync.WaitGroup{}
		wg2.Add(2)
		tests := map[string]*sync.WaitGroup{
			testOriginNamespace: wg1,
			testPeerNamespace:   wg2,
		}
		client := createConsulAPIClient(suite.T(), suite.container)
		saveBGStateNew(suite.T(), client, []string{testOriginNamespace, testPeerNamespace}, state1)

		for ns, wg := range tests {
			namespace := ns
			waitGroup := wg
			ctx := context.Background()
			publisherCtx, cancelPublisher := context.WithCancel(ctx)
			statePublisher, err := NewPublisher(publisherCtx, suite.container.endpoint, namespace, nil)
			assertions.Nil(err)
			subscribeCtx, cancelSubscriber := context.WithCancel(context.Background())
			counter := 0
			statePublisher.Subscribe(subscribeCtx, func(state BlueGreenState) {
				counter++
				if counter == 1 {
					var expectedState BlueGreenState
					if namespace == testOriginNamespace {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateActive},
							Sibling:    &NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateCandidate},
							UpdateTime: updateTimeState1,
						}
					} else {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateCandidate},
							Sibling:    &NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateActive},
							UpdateTime: updateTimeState1,
						}
					}
					assertions.Equal(expectedState, state)
					waitGroup.Done()
				} else if counter == 2 {
					var expectedState BlueGreenState
					if namespace == testOriginNamespace {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateLegacy},
							Sibling:    &NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateActive},
							UpdateTime: updateTimeState2,
						}
					} else {
						expectedState = BlueGreenState{
							Current:    NamespaceVersion{Namespace: testPeerNamespace, Version: NewVersionMust("v2"), State: StateActive},
							Sibling:    &NamespaceVersion{Namespace: testOriginNamespace, Version: NewVersionMust("v1"), State: StateLegacy},
							UpdateTime: updateTimeState2,
						}
					}
					assertions.Equal(expectedState, state)
					cancelSubscriber()
					cancelPublisher()
					waitGroup.Done()
				}
			})
		}
		saveBGStateNew(suite.T(), client, []string{testOriginNamespace, testPeerNamespace}, state2)
		assertions.True(waitWG(10*time.Second, wg1, wg2))
	})
}

func (suite *testSuite) TestStateChangedToTheSameVal() {
	withCleanConsul(suite, func() {
		assertions := require.New(suite.T())

		wg1 := &sync.WaitGroup{}
		wg1.Add(1)
		wg2 := &sync.WaitGroup{}
		wg2.Add(2)

		ctx := context.Background()
		publisherCtx, cancelPublisher := context.WithCancel(ctx)
		DefaultPollingWaitTime = time.Second
		statePublisher, err := NewPublisher(publisherCtx, suite.container.endpoint, testOriginNamespace, nil)
		assertions.Nil(err)
		subscribeCtx, cancelSubscriber := context.WithCancel(context.Background())
		counter := 0
		statePublisher.Subscribe(subscribeCtx, func(state BlueGreenState) {
			counter++
			if counter == 1 {
				wg1.Done()
				wg2.Done()
			} else if counter == 2 {
				wg2.Done()
			}
		})
		defer func() {
			cancelSubscriber()
			cancelPublisher()
		}()
		assertions.True(waitWG(3*time.Second, wg1))
		assertions.False(waitWG(3*time.Second, wg2))
	})
}

func createConsulAPIClient(t *testing.T, container *consulContainer) *api.Client {
	cfg := api.DefaultConfig()
	cfg.Address = container.endpoint
	client, err := api.NewClient(cfg)
	if nil != err {
		t.Fatal(err)
	}
	return client
}

func saveBGState(t *testing.T, client *api.Client, namespaces []string, state bgState) {
	for _, ns := range namespaces {
		stateJson, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.KV().Put(&api.KVPair{
			Key:   fmt.Sprintf(BgStateConsulPath, ns),
			Value: stateJson,
		}, nil); nil != err {
			t.Fatal(err)
		}
	}
}

func saveBGStateNew(t *testing.T, client *api.Client, namespaces []string, state bgState) {
	for _, ns := range namespaces {
		stateJson, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.KV().Put(&api.KVPair{
			Key:   fmt.Sprintf(BgStateConsulPathNew, ns),
			Value: stateJson,
		}, nil); nil != err {
			t.Fatal(err)
		}
	}
}

type consulContainer struct {
	testcontainers.Container
	endpoint string
}

func withCleanConsul(suite *testSuite, test func()) {
	client := createConsulAPIClient(suite.T(), suite.container)
	kv, err := listKeys(client, "")
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.T().Logf("kv list result before cleanup: %v", kv)
	_, err = client.KV().DeleteTree("", nil)
	if err != nil {
		suite.T().Fatal(err)
	}
	kv, err = listKeys(client, "")
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.T().Logf("kv list result after cleanup: %v", kv)
	test()
}

func listKeys(client *api.Client, prefix string) ([]string, error) {
	kv, _, err := client.KV().List(prefix, nil)
	if err != nil {
		return nil, err
	}
	unwrapKeys := func(kvs []*api.KVPair) []string {
		var result []string
		for _, kv := range kvs {
			if kv != nil {
				result = append(result, kv.Key)
			}
		}
		return result
	}
	return unwrapKeys(kv), nil
}
