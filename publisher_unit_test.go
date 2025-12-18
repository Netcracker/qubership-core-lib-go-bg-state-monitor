package blue_green_state_monitor_go

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"sync/atomic"
	"time"
	"errors"
)

func TestNewPath403_OldPath_404(t *testing.T) {
	newPathAttempts := 0
	oldPathAttempts := 0
	assertions := require.New(t)

	ts := createTestServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/kv/bluegreen/test-namespace-origin/bgstate") {
			newPathAttempts++
			w.WriteHeader(http.StatusForbidden)
		} else if strings.HasPrefix(r.URL.Path, "/v1/kv/config/test-namespace-origin/application/bluegreen/bgstate") {
			oldPathAttempts++
			w.Header().Add("X-Consul-Index", "100")
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()
	DefaultPollingWaitTime = time.Second
	publisher, err := NewPublisher(context.Background(), ts.URL, testOriginNamespace, func(ctx context.Context) (string, error) {
		return "test-token", nil
	})
	assertions.NoError(err)
	assertions.NotNil(publisher)
	state := publisher.GetState()
	assertions.NotNil(state)
	assertions.Equal(1, newPathAttempts)
	assertions.Equal(1, oldPathAttempts)
}

func TestNewPath403_OldPath_403(t *testing.T) {
	newPathAttempts := 0
	oldPathAttempts := 0
	assertions := require.New(t)

	ts := createTestServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/kv/bluegreen/test-namespace-origin/bgstate") {
			newPathAttempts++
			w.WriteHeader(http.StatusForbidden)
		} else if strings.HasPrefix(r.URL.Path, "/v1/kv/config/test-namespace-origin/application/bluegreen/bgstate") {
			oldPathAttempts++
			w.WriteHeader(http.StatusForbidden)
		}
	})
	defer ts.Close()
	DefaultPollingWaitTime = time.Second
	DefaultTimeoutDuration = 500 * time.Millisecond
	_, err := NewPublisher(context.Background(), ts.URL, testOriginNamespace, nil)
	assertions.Error(err)
	assertions.Equal(1, newPathAttempts)
	assertions.Equal(1, oldPathAttempts)
}

func TestNewPath403_OldPath_503(t *testing.T) {
	newPathAttempts := 0
	oldPathAttempts := 0
	assertions := require.New(t)

	ts := createTestServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/kv/bluegreen/test-namespace-origin/bgstate") {
			newPathAttempts++
			w.WriteHeader(http.StatusForbidden)
		} else if strings.HasPrefix(r.URL.Path, "/v1/kv/config/test-namespace-origin/application/bluegreen/bgstate") {
			oldPathAttempts++
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	defer ts.Close()
	DefaultPollingWaitTime = time.Second
	DefaultTimeoutDuration = 500 * time.Millisecond
	_, err := NewPublisher(context.Background(), ts.URL, testOriginNamespace, nil)
	assertions.Error(err)
	assertions.Equal(1, newPathAttempts)
	assertions.Equal(1, oldPathAttempts)
}

func TestNewPath503_OldPath_503(t *testing.T) {
	newPathAttempts := 0
	assertions := require.New(t)

	ts := createTestServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/kv/bluegreen/test-namespace-origin/bgstate") {
			newPathAttempts++
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	defer ts.Close()
	DefaultPollingWaitTime = time.Second
	DefaultTimeoutDuration = 500 * time.Millisecond
	_, err := NewPublisher(context.Background(), ts.URL, testOriginNamespace, nil)
	assertions.Error(err)
	assertions.Equal(1, newPathAttempts)
}

func createTestServer(fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(fn))
}

func TestNewPublisherWithConfig_Success(t *testing.T) {
	origBuild := buildTokenSupplierFunc
	origNew := newPublisherFunc
	defer func() {
		buildTokenSupplierFunc = origBuild
		newPublisherFunc = origNew
	}()

	buildTokenSupplierFunc = func(ctx context.Context, consulURL, namespace string) (func(context.Context) (string, error), error) {
		return func(context.Context) (string, error) {
			return "fake-token", nil
		}, nil
	}
	newPublisherFunc = func(ctx context.Context, consulURL, namespace string,
		tokenSupplier func(context.Context) (string, error)) (*ConsulBlueGreenStatePublisher, error) {
		return &ConsulBlueGreenStatePublisher{}, nil
	}

	ctx := context.Background()
	pub, err := NewPublisherWithConfig(ctx, "http://fake-consul", "test-ns")

	require.NoError(t, err)
	require.NotNil(t, pub)
}

func TestNewPublisherWithConfig_BuildTokenFails(t *testing.T) {
	origBuild := buildTokenSupplierFunc
	defer func() { buildTokenSupplierFunc = origBuild }()

	buildTokenSupplierFunc = func(ctx context.Context, consulURL, namespace string) (func(context.Context) (string, error), error) {
		return nil, errors.New("mock login failure")
	}

	ctx := context.Background()
	pub, err := NewPublisherWithConfig(ctx, "http://fake-consul", "test-ns")

	require.Error(t, err)
	require.Nil(t, pub)
}

func TestNewPublisherWithConfigBG_STATE_Success(t *testing.T) {
	origBuild := buildTokenSupplierFunc
	origNew := newPublisherFunc
	defer func() {
		buildTokenSupplierFunc = origBuild
		newPublisherFunc = origNew
	}()

	buildTokenSupplierFunc = func(ctx context.Context, consulURL, namespace string) (func(context.Context) (string, error), error) {
		return func(context.Context) (string, error) {
			return "fake-token", nil
		}, nil
	}

	newPublisherFunc = func(ctx context.Context, consulURL, namespace string,
		tokenSupplier func(context.Context) (string, error)) (*ConsulBlueGreenStatePublisher, error) {

		ptr := &atomic.Pointer[BlueGreenState]{}
		ptr.Store(&BlueGreenState{})

		return &ConsulBlueGreenStatePublisher{
			statePointer: ptr,
		}, nil
	}

	ctx := context.Background()
	pub, err := NewPublisherWithConfig(ctx, "http://fake-consul", "test-ns")

	require.NoError(t, err)
	require.NotNil(t, pub)

	state := pub.GetState()
	require.NotNil(t, state)
}
