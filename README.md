[![Go build](https://github.com/Netcracker/qubership-core-lib-go-bg-state-monitor/actions/workflows/go-build.yml/badge.svg)](https://github.com/Netcracker/qubership-core-lib-go-bg-state-monitor/actions/workflows/go-build.yml)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?metric=coverage&project=Netcracker_qubership-core-lib-go-bg-state-monitor)](https://sonarcloud.io/summary/overall?id=Netcracker_qubership-core-lib-go-bg-state-monitor)
[![duplicated_lines_density](https://sonarcloud.io/api/project_badges/measure?metric=duplicated_lines_density&project=Netcracker_qubership-core-lib-go-bg-state-monitor)](https://sonarcloud.io/summary/overall?id=Netcracker_qubership-core-lib-go-bg-state-monitor)
[![vulnerabilities](https://sonarcloud.io/api/project_badges/measure?metric=vulnerabilities&project=Netcracker_qubership-core-lib-go-bg-state-monitor)](https://sonarcloud.io/summary/overall?id=Netcracker_qubership-core-lib-go-bg-state-monitor)
[![bugs](https://sonarcloud.io/api/project_badges/measure?metric=bugs&project=Netcracker_qubership-core-lib-go-bg-state-monitor)](https://sonarcloud.io/summary/overall?id=Netcracker_qubership-core-lib-go-bg-state-monitor)
[![code_smells](https://sonarcloud.io/api/project_badges/measure?metric=code_smells&project=Netcracker_qubership-core-lib-go-bg-state-monitor)](https://sonarcloud.io/summary/overall?id=Netcracker_qubership-core-lib-go-bg-state-monitor)

# blue-green-state-monitor-go

## Usage with fixed consul token

```go
package main

import (
	"context"
	"fmt"
	"github.com/netcracker/qubership-core-lib-go-bg-state-monitor/v2"
)

func demo(ctx context.Context, consulUrl string, consulToken string) {
	publisher, err := blue_green_state_monitor_go.NewPublisher(ctx,
		consulUrl, "test-namespace", func(ctx context.Context) (string, error) { return consulToken, nil })
	if err != nil {
		panic(err)
	}
	ctxS, cancel := context.WithCancel(ctx)
	publisher.Subscribe(ctxS, func(state blue_green_state_monitor_go.BlueGreenState) {
		fmt.Printf("state=%s\n", state)
	})
	state := publisher.GetState()
	fmt.Printf("state=%s\n", state.String())
	cancel()
}
```

## Usage with m2m consul token provider
```go
package main

import (
	"context"
	"fmt"
	bgState "github.com/netcracker/qubership-core-lib-go-bg-state-monitor/v2"
)

func demo(ctx context.Context, consulUrl string, consulToken string, namespace string) {
	publisher, err := bgState.NewPublisher(ctx,
		consulUrl, namespace, func(ctx context.Context) (string, error) { return consulToken, nil })
	if err != nil {
		panic(err)
	}
	ctxS, cancel := context.WithCancel(ctx)
	publisher.Subscribe(ctxS, func(state bgState.BlueGreenState) {
		fmt.Printf("state=%s\n", state)
	})
	state := publisher.GetState()
	fmt.Printf("state=%s\n", state.String())
	cancel()
}
```
