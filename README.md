# SOLANA RPC TESTS

This package is designed for load testing Solana RPC providers.

### Building
```bash
go build ./cmd/load_testing.go
```

### Running
```bash
./load_testing --providerUrl https://solana-mainnet.rpc.extrnode.com/your_key --totalRequests 1000
```

### Parameters

- **--providerUrl**:
  - **Description:** Specifies the URL for the provider to be tested.
  - **Type:** string
  - **Usage:** This flag allows you to provide the URL of the service or resource you want to test.

- **--rateLimit**:
  - **Description:** Sets the rate limit for requests to the provider.
  - **Type:** uint
  - **Usage:** Determines the maximum number of requests allowed per unit of time (usually per second) to be sent to the provider

- **--totalRequests**:
  - **Description:** Specifies the total number of test requests to be sent.
  - **Type:** uint
  - **Usage:** Defines the total number of requests that will be made during the test. This flag allows you to customize the workload for the test.

- **--reqPerMethod**:
  - **Description:** Sets the number of repeated tests for each request.
  - **Type:** uint
  - **Usage:** Determines how many times each request will be repeated during the test. For example, if set to 5, each request will be sent 5 times. This can be useful for assessing the stability and performance of the provider over multiple executions of the same request.