package blue_green_state_monitor_go

import (
	"context"
	"errors"
	"testing"
	"time"
    restConsul "github.com/netcracker/qubership-core-lib-go-rest-utils/v2/consul-propertysource"
	"github.com/stretchr/testify/require"
)

func TestBuildTokenSupplier_UsesProvidedConsulToken(t *testing.T) {
	tkn := "provided-token"

	origTokenFunc := getConsulTokenFunc
	getConsulTokenFunc = func() string { return tkn }
	t.Cleanup(func() { getConsulTokenFunc = origTokenFunc })

	origFactory := newRestConsulClient
	newRestConsulClient = func(restConsul.ClientConfig) consulClient {
		t.Fatalf("unexpected consul client creation when token is provided")
		return nil
	}
	t.Cleanup(func() { newRestConsulClient = origFactory })

	supplier, err := buildTokenSupplier(context.Background(), "http://fake-consul", "test-ns")
	require.NoError(t, err)
	require.NotNil(t, supplier)

	token, err := supplier(context.Background())
	require.NoError(t, err)
	require.Equal(t, tkn, token)
}

func TestBuildTokenSupplier_LoginSuccessAfterRetry(t *testing.T) {
	origTokenFunc := getConsulTokenFunc
	getConsulTokenFunc = func() string { return "" }
	t.Cleanup(func() { getConsulTokenFunc = origTokenFunc })

	client := &stubConsulClient{
		secretID:         "login-token",
		loginErrSequence: []error{errors.New("login failed"), nil},
	}

	origFactory := newRestConsulClient
	newRestConsulClient = func(restConsul.ClientConfig) consulClient {
		return client
	}
	t.Cleanup(func() { newRestConsulClient = origFactory })

	origRetryCount := consulRetryCount
	origRetryDelay := consulRetryDelay
	consulRetryCount = 3
	consulRetryDelay = time.Duration(0)
	t.Cleanup(func() {
		consulRetryCount = origRetryCount
		consulRetryDelay = origRetryDelay
	})

	supplier, err := buildTokenSupplier(context.Background(), "http://fake-consul", "test-ns")
	require.NoError(t, err)
	require.Equal(t, 2, client.loginCalls)

	token, err := supplier(context.Background())
	require.NoError(t, err)
	require.Equal(t, "login-token", token)
}

func TestBuildTokenSupplier_LoginFailsAfterRetries(t *testing.T) {
	origTokenFunc := getConsulTokenFunc
	getConsulTokenFunc = func() string { return "" }
	t.Cleanup(func() { getConsulTokenFunc = origTokenFunc })

	loginErr := errors.New("permanent login failure")
	client := &stubConsulClient{
		secretID:   "",
		defaultErr: loginErr,
	}

	origFactory := newRestConsulClient
	newRestConsulClient = func(restConsul.ClientConfig) consulClient {
		return client
	}
	t.Cleanup(func() { newRestConsulClient = origFactory })

	origRetryCount := consulRetryCount
	origRetryDelay := consulRetryDelay
	consulRetryCount = 2
	consulRetryDelay = time.Duration(0)
	t.Cleanup(func() {
		consulRetryCount = origRetryCount
		consulRetryDelay = origRetryDelay
	})

	supplier, err := buildTokenSupplier(context.Background(), "http://fake-consul", "test-ns")
	require.Error(t, err)
	require.Nil(t, supplier)
	require.Equal(t, consulRetryCount, client.loginCalls)
}

type stubConsulClient struct {
	secretID         string
	loginErrSequence []error
	defaultErr       error
	loginCalls       int
}

func (s *stubConsulClient) Login() error {
	s.loginCalls++
	if len(s.loginErrSequence) >= s.loginCalls {
		return s.loginErrSequence[s.loginCalls-1]
	}
	return s.defaultErr
}

func (s *stubConsulClient) SecretId() string {
	return s.secretID
}
