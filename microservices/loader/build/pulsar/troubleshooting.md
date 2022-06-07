

```
(base) welcome@Traianos-MacBook-Pro consumer % dig +short trading-api.dexp-qa.com
(base) welcome@Traianos-MacBook-Pro consumer % 
(base) welcome@Traianos-MacBook-Pro consumer % ping trading-api.dexp-qa.com
ping: cannot resolve trading-api.dexp-qa.com: Unknown host
(base) welcome@Traianos-MacBook-Pro consumer % 
```


```
func getOrders(limit int, secret_key string, api_key string, base_url string, request_id string) {

	orderGetParameters := `{"userId":` + request_id + `,limit":` + strconv.Itoa(limit) + `}`

	//Request body for POSTing a Trade
	params, err := json.Marshal(orderGetParameters)

	if err != nil {
		fmt.Println("(getOrders) failed to jsonify: ", params)
	}

	requestString := string(params)

	//debug
	fmt.Println("(getOrders) request parameters -> ", requestString)
	sig := sign_api_request(secret_key, requestString)

	//debug
	fmt.Println("(getOrders) request signature -> ", sig)
	trade_request_url := "https://" + base_url + "/api/getOrders"

	//Set the client connection custom properties
	fmt.Println("(getOrders) setting client connection properties.")
	client := http.Client{}

	//POST body
	fmt.Println("(getOrders) creating new POST request: ")
	request, err := http.NewRequest("POST", trade_request_url, bytes.NewBuffer(params))

	//set some headers
	fmt.Println("(getOrders) setting request headers ...")
	request.Header.Set("Content-type", "application/json")
	request.Header.Set("requestToken", api_key)
	request.Header.Set("signature", sig)

	if err != nil {
		fmt.Println("(getOrders) error after header addition: ", err.Error())
	}

	fmt.Println("(getOrders) executing the POST to ", trade_request_url)
	resp, err := client.Do(request)

	if err != nil {
		fmt.Println("(getOrders) error after executing POST", err.Error())
	}

	defer resp.Body.Close()
	fmt.Println("(getOrders) reading response body ...")
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(getOrders) error reading response body: ")
		log.Fatalln(err)
	}

	sb := string(body)

	fmt.Println("(getOrders) got response output: ", sb)

}









```
(base) welcome@Traianos-MacBook-Pro eks-deploy % kubectl get events -n ragnarok| egrep -i consumer| egrep -i warning
(base) welcome@Traianos-MacBook-Pro eks-deploy % watch "kubectl get pods -n ragnarok | egrep consumer"              
(base) welcome@Traianos-MacBook-Pro eks-deploy % kubectl logs consumer-5df8cfd787-szk2f  -n ragnarok --follow
{ "login":"ngocdf1_qa_indi_7uxp@mailinator.com",  "password":"Eqonex@123456",  "userId":"2661"}
#debug: getting request response body:  {"error":"rate limit exceeded"}
#debug: getting request ID:  0
#debug: getting api_key:  
#debug: getting secret_key:  
(main) contemplating running main worker loop ...
1650770025098985697 [host=consumer-5df8cfd787-szk2f](consume_payload_data) worker 1 consuming from topic ragnarok/transactions/requests (consume_payload_data)
time="2022-04-24T03:13:45Z" level=info msg="[Connecting to broker]" remote_addr="pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="[TCP connection established]" local_addr="192.168.7.200:44222" remote_addr="pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="[Connection is ready]" local_addr="192.168.7.200:44222" remote_addr="pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="Found connection in pool key=pulsar-broker.pulsar.svc.cluster.local:665045 0 logical_addr=pulsar://pulsar-broker.pulsar.svc.cluster.local:6650 physical_addr=pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="[Connecting to broker]" remote_addr="pulsar://pulsar-broker-4.pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="[TCP connection established]" local_addr="192.168.7.200:37954" remote_addr="pulsar://pulsar-broker-4.pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=info msg="[Connection is ready]" local_addr="192.168.7.200:37954" remote_addr="pulsar://pulsar-broker-4.pulsar-broker.pulsar.svc.cluster.local:6650"
time="2022-04-24T03:13:45Z" level=error msg="[Failed to create consumer]" consumerID=1 error="server error: PersistenceError: java.util.concurrent.CompletionException: java.lang.NullPointerException" name=hfjao subscription=sub001 topic="persistent://ragnarok/transactions/requests"
time="2022-04-24T03:13:45Z" level=error msg="[Failed to create consumer]" consumerID=1 error="server error: PersistenceError: java.util.concurrent.CompletionException: java.lang.NullPointerException" name=hfjao subscription=sub001 topic="persistent://ragnarok/transactions/requests"
2022/04/24 03:13:45 server error: PersistenceError: java.util.concurrent.CompletionException: java.lang.NullPointerException
```


#Troubleshooting ingestor service

See graphs: "Redis Transaction Performance", "Source Order Data Loader", "Work Allocation Table", "Input file generator"

```
[log] /applogs/loader.log 1650715316992748188 Loading past captured orders ...
(loadHistoricalData) Getting ingestor service status:  done
triggered ingestion start:  
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  started
(loadHistoricalData) Getting ingestor service status:  working
(loadHistoricalData) Getting ingestor service status:  working
(loadHistoricalData) Getting ingestor service status:  working
(loadHistoricalData) Getting ingestor service status:  working
(loadHistoricalData) Getting ingestor service status:  working
(loadHistoricalData) Getting ingestor service status:  working
.
.
.
(purgeProcessedRedis) dummy purging redis item  42845
(purgeProcessedRedis) deleted input 42845 -> %!s(int64=0)
(purgeProcessedRedis) dummy purging redis item  36059
(purgeProcessedRedis) deleted input 36059 -> %!s(int64=0)
(purgeProcessedRedis) dummy purging redis item  32900
(purgeProcessedRedis) deleted input 32900 -> %!s(int64=0)
(purgeProcessedRedis) dummy purging redis item  37494
(purgeProcessedRedis) deleted input 37494 -> %!s(int64=0)
(purgeProcessedRedis) dummy purging redis item  7222
(purgeProcessedRedis) deleted input 7222 -> %!s(int64=0)
[log] /applogs/loader.log 1650717117770239662 (purgeProcessedRedis) purged processed files: 76947
(purgeProcessedRedis) Switched back to default REDIS DB:  0
[log] (process_input_data_redis_concurrent) 1650717117771211717 leaving routing process input data redis concurrent
[log] (process_input_data_redis_concurrent) 1650717117771218852 task count = 320 workcount = 20 numWorkers = 20

```

#Troubleshooting for the streamer service

```


```

- Checking the loader service

```


```

- Checking the streamer service:

```


```


- Checking the Producer service:


```


```


- Checking the consumer service:


```


```


- Checking the ingestor service



```


```


- Checking Pulsar queueing service



```




```


- Checking REDIS DB

```


```



- Checking S3 bucket data source


```



```



- Checking Grafana Service:


```



```


- Checking Prometheus Service:

```


```



- Checking Ingress Service

```



```


- Troubleshooting EKS Platform


```


```