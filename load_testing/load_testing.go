package load_testing

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

const (
	reqPerMethod = 1
)

type BenchmarkResult struct {
	URL            string
	Method         string
	MinTimings     RequestTimings
	MaxTimings     RequestTimings
	AverageTimings RequestTimings
	TotalTimings   RequestTimings
	TotalTime      time.Duration
	RequestsCount  int
	Params         interface{}
}

type AggregateBenchmarkResult struct {
	URL              string
	Method           string
	TotalTime        time.Duration
	RequestsCount    int
	AggregateTimings RequestTimings
	MaxTimings       RequestTimings
	MinTimings       RequestTimings
}

type RequestTimings struct {
	DNSLookup        time.Duration
	TCPConnection    time.Duration
	TLSHandshake     time.Duration
	ServerProcessing time.Duration
}

func performRequest(client *http.Client, url string, request []byte, limiter *rate.Limiter) (time.Duration, RequestTimings, error) {
	var (
		dnsStart, connectStart, tlsStart                            time.Time
		dnsDuration, connectDuration, serverProcessing, tlsDuration time.Duration
		timings                                                     RequestTimings
	)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(request)) //nolint:noctx
	if err != nil {
		return 0, timings, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Wait for limiter
	err = limiter.Wait(context.Background())
	if err != nil {
		return 0, timings, err
	}

	start := time.Now()
	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			dnsDuration = time.Since(dnsStart)
		},
		ConnectStart: func(_, _ string) {
			connectStart = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			connectDuration = time.Since(connectStart)
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			tlsDuration = time.Since(tlsStart)
		},
		GotFirstResponseByte: func() {
			serverProcessing = time.Since(start) - connectDuration - dnsDuration - tlsDuration
		},
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	response, err := client.Do(req)
	if err != nil {
		return 0, timings, err
	}
	defer response.Body.Close()
	if serverProcessing < 0 {
		return 0, timings, fmt.Errorf("invalid calculation of serverProcessing")
	}

	// read the response body
	resp, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, timings, err
	}
	if response.StatusCode != http.StatusOK {
		return 0, timings, errors.New(response.Status)
	}
	requestTime := time.Since(start)

	var r jsonrpc.RPCResponse
	err = json.Unmarshal(resp, &r)
	if err != nil {
		return 0, RequestTimings{}, err
	}
	if r.Error != nil {
		return 0, RequestTimings{}, r.Error
	}

	timings = RequestTimings{
		DNSLookup:        dnsDuration,
		TCPConnection:    connectDuration,
		ServerProcessing: serverProcessing,
		TLSHandshake:     tlsDuration,
	}

	return requestTime, timings, nil
}

func benchmarkMethod(url, method string, params interface{}, client *http.Client, request []byte, limiter *rate.Limiter) BenchmarkResult {
	var totalTime time.Duration
	var minTimings, maxTimings, totalTimings, avgTimings RequestTimings

	var performedRequests int
	for i := 0; i < reqPerMethod; i++ {
		requestTime, timings, err := performRequest(client, url, request, limiter)

		if err != nil {
			log.Printf("ERROR: URL: %s, ERR: %s", url, err)
			continue
		}

		if performedRequests == 0 {
			minTimings = timings
			maxTimings = timings
		} else {
			minTimings.DNSLookup = minDuration(minTimings.DNSLookup, timings.DNSLookup)
			minTimings.TCPConnection = minDuration(minTimings.TCPConnection, timings.TCPConnection)
			minTimings.ServerProcessing = minDuration(minTimings.ServerProcessing, timings.ServerProcessing)
			minTimings.TLSHandshake = minDuration(minTimings.TLSHandshake, timings.TLSHandshake)

			maxTimings.DNSLookup = maxDuration(maxTimings.DNSLookup, timings.DNSLookup)
			maxTimings.TCPConnection = maxDuration(maxTimings.TCPConnection, timings.TCPConnection)
			maxTimings.ServerProcessing = maxDuration(maxTimings.ServerProcessing, timings.ServerProcessing)
			maxTimings.TLSHandshake = maxDuration(maxTimings.TLSHandshake, timings.TLSHandshake)
		}

		totalTimings.DNSLookup += timings.DNSLookup
		totalTimings.TCPConnection += timings.TCPConnection
		totalTimings.ServerProcessing += timings.ServerProcessing
		totalTimings.TLSHandshake += timings.TLSHandshake

		totalTime += requestTime
		performedRequests++
	}

	if performedRequests > 0 {
		avgTimings.DNSLookup = time.Duration(int(totalTimings.DNSLookup) / performedRequests)
		avgTimings.TCPConnection = time.Duration(int(totalTimings.TCPConnection) / performedRequests)
		avgTimings.ServerProcessing = time.Duration(int(totalTimings.ServerProcessing) / performedRequests)
		avgTimings.TLSHandshake = time.Duration(int(totalTimings.TLSHandshake) / performedRequests)
	}

	return BenchmarkResult{
		URL:            url,
		Method:         method,
		MinTimings:     minTimings,
		MaxTimings:     maxTimings,
		AverageTimings: avgTimings,
		TotalTimings:   totalTimings,
		TotalTime:      totalTime,
		RequestsCount:  performedRequests,
		Params:         params,
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func StartTest(providerURL string, rateLimit, totalRequests uint) {
	file, err := os.OpenFile("./test_competitors.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666) //nolint:revive
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	log.SetOutput(file)

	resultsMutex := &sync.Mutex{}
	results := make(chan BenchmarkResult)
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: false, // use persistent connections
		},
	}

	aggregateResults := make(map[string]*AggregateBenchmarkResult)
	var (
		wg sync.WaitGroup
	)
	for i := 0; i < int(totalRequests); i++ {
		accountIndex := i % len(AccountKeys)
		wg.Add(1)
		go func(url string, accountKeys []string, accountIndex int) {
			defer wg.Done()
			var k interface{} = accountKeys[accountIndex]
			params := []interface{}{k, map[string]interface{}{
				"encoding":   "jsonParsed",
				"commitment": "finalized",
			}}
			reqBody, err := json.Marshal(jsonrpc.NewRequest(GetAccountInfo, params)) //nolint:asasalint
			if err != nil {
				log.Errorf("Marshal: %s", err)
				return
			}
			resp := benchmarkMethod(url, GetAccountInfo, params, client, reqBody, rate.NewLimiter(rate.Limit(rateLimit), 1))
			if resp.RequestsCount != 0 {
				results <- resp
			}
		}(providerURL, AccountKeys, accountIndex)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		log.Printf("URL: %s, Method: %s, MinTimings: %+v, MaxTimings: %+v, AverageTimings: %+v, TotalTime: %s, RequestsCount: %d, Params: %+v\n", result.URL, result.Method, result.MinTimings, result.MaxTimings, result.AverageTimings, result.TotalTime, result.RequestsCount, result.Params)

		resultsMutex.Lock()
		key := fmt.Sprintf("%s_%s", result.URL, result.Method)
		aggregateResult, ok := aggregateResults[key]
		if !ok {
			aggregateResults[key] = &AggregateBenchmarkResult{
				URL:              result.URL,
				Method:           result.Method,
				MinTimings:       result.MinTimings,
				MaxTimings:       result.MaxTimings,
				AggregateTimings: result.TotalTimings,
				TotalTime:        result.TotalTime,
				RequestsCount:    result.RequestsCount,
			}
		} else {
			aggregateResult.TotalTime += result.TotalTime
			aggregateResult.RequestsCount += result.RequestsCount
			aggregateResult.AggregateTimings.DNSLookup += result.TotalTimings.DNSLookup
			aggregateResult.AggregateTimings.TCPConnection += result.TotalTimings.TCPConnection
			aggregateResult.AggregateTimings.ServerProcessing += result.TotalTimings.ServerProcessing
			aggregateResult.AggregateTimings.TLSHandshake += result.TotalTimings.TLSHandshake

			aggregateResult.MinTimings.DNSLookup = minDuration(aggregateResult.MinTimings.DNSLookup, result.MinTimings.DNSLookup)
			aggregateResult.MinTimings.TCPConnection = minDuration(aggregateResult.MinTimings.TCPConnection, result.MinTimings.TCPConnection)
			aggregateResult.MinTimings.ServerProcessing = minDuration(aggregateResult.MinTimings.ServerProcessing, result.MinTimings.ServerProcessing)
			aggregateResult.MinTimings.TLSHandshake = minDuration(aggregateResult.MinTimings.TLSHandshake, result.MinTimings.TLSHandshake)

			aggregateResult.MaxTimings.DNSLookup = maxDuration(aggregateResult.MaxTimings.DNSLookup, result.MaxTimings.DNSLookup)
			aggregateResult.MaxTimings.TCPConnection = maxDuration(aggregateResult.MaxTimings.TCPConnection, result.MaxTimings.TCPConnection)
			aggregateResult.MaxTimings.ServerProcessing = maxDuration(aggregateResult.MaxTimings.ServerProcessing, result.MaxTimings.ServerProcessing)
			aggregateResult.MaxTimings.TLSHandshake = maxDuration(aggregateResult.MaxTimings.TLSHandshake, result.MaxTimings.TLSHandshake)
		}
		resultsMutex.Unlock()
	}

	for _, aggregateResult := range aggregateResults {
		aggregateResult.AggregateTimings.DNSLookup = time.Duration(int(aggregateResult.AggregateTimings.DNSLookup) / aggregateResult.RequestsCount)
		aggregateResult.AggregateTimings.TCPConnection /= time.Duration(aggregateResult.RequestsCount)
		aggregateResult.AggregateTimings.ServerProcessing /= time.Duration(aggregateResult.RequestsCount)
		aggregateResult.AggregateTimings.TLSHandshake /= time.Duration(aggregateResult.RequestsCount)

		log.Printf("TOTAL METHOD RESULT: URL: %s, Method: %s, MinTimings: %+v, MaxTimings: %+v, AverageTimings: %+v, TotalTime: %s, RequestsCount: %d\n", aggregateResult.URL, aggregateResult.Method, aggregateResult.MinTimings, aggregateResult.MaxTimings, aggregateResult.AggregateTimings, aggregateResult.TotalTime, aggregateResult.RequestsCount)
	}
}
