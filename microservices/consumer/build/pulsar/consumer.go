package main

/* Consumer Pool Management Service*/

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//REDIS related parameters for credential lookups
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var redisReadConnectionAddress string = os.Getenv("REDIS_REPLICA_ADDRESS") //address:port combination e.g  "my-release-redis-replicas.default.svc.cluster.local:6379"
var redisAuthPass string = os.Getenv("REDIS_PASS")
var credentialsDBindex int = 14

//Get script configuration from the shell environment
var numJobs, _ = strconv.Atoi(os.Getenv("NUM_JOBS"))       //20
var numWorkers, _ = strconv.Atoi(os.Getenv("NUM_WORKERS")) //20
var port_specifier string = ":" + os.Getenv("PORT_NUMBER") // /var/log

var pulsarBrokerURL = os.Getenv("PULSAR_BROKER_SERVICE_ADDRESS")      // e.g "????"
var subscriptionName = os.Getenv("PULSAR_CONSUMER_SUBSCRIPTION_NAME") //e.g sub001

var topic0 string = os.Getenv("MESSAGE_TOPIC") // "messages" or  "ragnarok/requests/transactions"
var topic1 string = os.Getenv("ERROR_TOPIC")   // "api-failures"
var topic2 string = os.Getenv("METRICS_TOPIC") // "metrics"

var target_api_url string = os.Getenv("TARGET_API_URL") // e.g To use the dummy target api, provide: http://<some_ip_address>:<someport>/orders

var hostname string = os.Getenv("HOSTNAME")                            // "the pod hostname (in k8s) which ran this instance of go"
var logFile string = os.Getenv("LOCAL_LOGFILE_PATH") + "/consumer.log" // "/data/applogs/consumer.log"
var consumer_group = os.Getenv("HOSTNAME")                             // we set the consumer group name to the podname / hostname

//API login details
var base_url string = os.Getenv("API_BASE_URL") //"trading-api.dexp-qa.com"
var username, password string

var userID int = 2661                                          // really arbitrary placehodler
var clOrdId string = os.Getenv("TRADING_API_CLORID")           //"test-1-traiano45"
var blockWaitAck, _ = strconv.Atoi(os.Getenv("BLOCKWAIT_ACK")) //blockwaitack
var account int = 0                                            //updated with the value of requestID for each new login to the API

var batchIndex int = 0 //counter for batching up cancels
var cancelMap map[int]string
var cancelBatchLimit, _ = strconv.Atoi(os.Getenv("CANCEL_BATCH_LIMIT"))
var cancelAllThreshold, _ = strconv.Atoi(os.Getenv("CANCEL_ALL_THRESHOLD")) //cancelAllThreshold - run a cancellall after this many placed orders

//Global Error Counter during lifetime of this service run
var errorCount int = 0
var requestCount int = 0

type APIResponse struct {
	Orders []Order `json:"orders"`
}

type Order struct {
	OrderId      int `json:"orderId"`
	UserId       int `json:"userId"`
	InstrumentId int `json:"instrumentId"`
}

type Credential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserId   int    `json:"userId"`
}

type User struct {
	Username string `redis:"username"`
	Password string `redis:"password"`
	Userid   string `redis:"userid"`
	Used     string `redis:"used"`
}

type Payload struct {
	InstrumentId   string `json:"instrumentId"`
	Symbol         string `json:"symbol"`
	UserId         string `json:"userId"`
	Side           string `json:"side"`
	OrdType        string `json:"ordType"`
	Price          string `json:"price"`
	Price_scale    string `json:"price_scale"`
	Quantity       string `json:"quantity"`
	Quantity_scale string `json:"quantity_scale"`
	Nonce          string `json:"nonce"`
	BlockWaitAck   string `json:"blockWaitAck "`
	ClOrdId        string `json:"clOrdId"`
}

//Logon to the API
type authCredential struct {
	Id            int    `json:"id"`
	RequestToken  string `json:"requestToken"`
	RequestSecret string `json:"requestSecret"`
}

//to process the respnse for just the fields we need
type Response struct {
	Id     int    `json:"id"`
	Status string `json:"status"`
}

//We need to eventually replace this with a Golang native method as callouts to python
//are inefficient
func sign_api_request(apiSecret string, requestBody string) (s string) {
	//This is a very nasty workaround with potentially negative performance implications
	out, err := exec.Command("/usr/bin/python3", "use.py", apiSecret, requestBody).Output()

	if err != nil {
		fmt.Println("sign_api_request error!", err)
	}

	s = strings.TrimSuffix(string(out), "\n")
	return s
}

//4. Build up the request body
func create_order(secret_key string, api_key string, base_url string, orderParameters map[string]string, orderIndex int, request_id string, userId int) {

	//Request body for POSTing a Trade
	params, err := json.Marshal(orderParameters)

	if err != nil {
		fmt.Println("(create_order) failed to jsonify: ", "(", orderIndex, ")", params)
	}

	requestString := string(params)

	//debug
	fmt.Println("(create_order) request parameters -> ", "(", orderIndex, ")", requestString)
	sig := sign_api_request(secret_key, requestString)

	//debug
	fmt.Println("(create_order) request signature -> ", "(", orderIndex, ")", sig)

	trade_request_url := "https://" + base_url + "/api/order"

	//Set the client connection custom properties
	fmt.Println("(create_order) setting client connection properties.", "(", orderIndex, ")")
	client := http.Client{}

	//POST body
	fmt.Println("(create_order) creating new POST request: ")
	request, err := http.NewRequest("POST", trade_request_url, bytes.NewBuffer(params))

	//set some headers
	fmt.Println("(create_order) setting request headers ...")
	request.Header.Set("Content-type", "application/json")
	request.Header.Set("requestToken", api_key)
	request.Header.Set("signature", sig)

	if err != nil {
		fmt.Println("(create_order) error after header addition: ", "(", orderIndex, ")")
		log.Fatalln(err)
	}

	fmt.Println("(create_order) executing the POST to ", "(", orderIndex, ")", trade_request_url)
	resp, err := client.Do(request)

	if err != nil {
		fmt.Println("(create_order) error after executing POST: ", "(", orderIndex, ")")
		log.Fatalln(err)

		//record as a failure metric
		recordFailedMetrics()
	}

	defer resp.Body.Close()
	fmt.Println("(create_order) reading response body ...", "(", orderIndex, ")")
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(create_order) error reading response body: ", "(", orderIndex, ")")
		log.Fatalln(err)
	}

	sb := string(body)

	fmt.Println("(create_order) got response output: ", "(", orderIndex, ")", sb)

	batchCancels(sb, orderParameters, secret_key, api_key, request_id, userId) //batchCancels(stringBody string, orderParameters map[string]string, secret_key string, api_key string, request_id int)

	//record this as a success metric
	recordSuccessMetrics()

}

//little wrapper function to ease the pain ...
func batchCancels(stringBody string, orderParameters map[string]string, secret_key string, api_key string, request_id string, userId int) {

	batchIndex++

	matchString := `sent`

	data := Response{}

	json.Unmarshal([]byte(stringBody), &data)

	if strings.Contains(data.Status, matchString) {

		//batch up the successful orders for later cancellations
		fmt.Println("(batchCancels) logging sent order: id = ", data.Id, " status = ", data.Status, " limit: ", batchIndex, "==", cancelBatchLimit)

		if batchIndex == cancelBatchLimit {

			fmt.Println("(batchCancels) reached batch limit ", batchIndex)

			requestParams := make(map[string]string)
			requestParams["userId"] = strconv.Itoa(userId)
			requestParams["limit"] = strconv.Itoa(cancelBatchLimit)

			getOrders(batchIndex, secret_key, api_key, base_url, requestParams)

			//reset the batch count
			batchIndex = 0

		}

	} else {
		fmt.Println("(batchCancels) skip: ", batchIndex, " response content body ", stringBody)
	}
}

func getOrders(batchIndex int, secret_key string, api_key string, base_url string, requestParameters map[string]string) {

	//Request body for POSTing a Trade
	params, err := json.Marshal(requestParameters)

	if err != nil {
		fmt.Println("(getOrders) 1) failed to jsonify: ", params)
	}

	requestString := string(params)

	//debug
	fmt.Println("(getOrders) 2) request parameters -> ", requestString)
	sig := sign_api_request(secret_key, requestString)

	//debug
	fmt.Println("(getOrders) 3) request signature -> ", sig)
	trade_request_url := "https://" + base_url + "/api/getOrders"

	//Set the client connection custom properties
	fmt.Println("(getOrders) 4) setting client connection properties.")

	client := http.Client{}

	//POST body
	fmt.Println("(getOrders) 5) creating new POST request: ")
	request, err := http.NewRequest("POST", trade_request_url, bytes.NewBuffer(params))
	//set some headers

	fmt.Println("(getOrders) 6) setting request headers ...")

	request.Header.Set("Content-type", "application/json")
	request.Header.Set("requestToken", api_key)
	request.Header.Set("signature", sig)

	if err != nil {

		fmt.Println("(getOrders) 7) error after header addition: ", err.Error())

	} else {

		//Execute the post
		fmt.Println("(getOrders) 7) executing the POST ...", request)

		resp, err := client.Do(request)

		if err != nil {

			fmt.Println("(getOrders) 8) error after executing POST: ", err.Error())

		} else {

			fmt.Println("(getOrders) 8) response after executing POST: ", resp)

		}

		defer resp.Body.Close()

		fmt.Println("(getOrders) 9) reading response body ...")

		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {

			fmt.Println("(getOrders) 10) error reading response body: ", err.Error())

		} else {

			sb := string(body)
			fmt.Println("(getOrders) 10) got response output: ", sb)

		}

		var response APIResponse

		json.Unmarshal(body, &response)

		fmt.Println("(getOrders) 11) response marshal: ", response)

		orderCancelParameters := make(map[string]string)

		for orderIndex, p := range response.Orders {
			fmt.Println("(getOrders) 12) listing past orders for cancellation: ", orderIndex, " orderId = ", p.OrderId, " UserId = ", p.UserId, " InstrumentId = ", p.InstrumentId)

			//{"instrumentId": instrument_id,"userId": user_id, "clOrdId": order_id}

			orderCancelParameters["instrumentId"] = strconv.Itoa(p.InstrumentId)
			orderCancelParameters["userId"] = strconv.Itoa(p.UserId)
			orderCancelParameters["clOrdId"] = strconv.Itoa(p.OrderId)

			cancel_order(secret_key, api_key, base_url, orderCancelParameters, orderIndex)
		}

	}

}

//MODIFY to cancel by order ID (order_id,api_key,secret_key,user_id,instrument_id)
func cancel_order(secret_key string, api_key string, base_url string, orderCancelParameters map[string]string, orderIndex int) {

	//Request body for POSTing a Trade
	params, err := json.Marshal(orderCancelParameters)

	if err != nil {
		fmt.Println("(cancel_order) failed to jsonify: ", "(", orderIndex, ")", params)
	}

	requestString := string(params)

	//debug
	fmt.Println("(cancel_order) request parameters -> ", "(", orderIndex, ")", requestString)
	sig := sign_api_request(secret_key, requestString)

	//debug
	fmt.Println("(cancel_order) request signature -> ", "(", orderIndex, ")", sig)

	trade_request_url := "https://" + base_url + "/api/cancelOrder"

	//Set the client connection custom properties
	fmt.Println("(cancel_order) setting client connection properties.", "(", orderIndex, ")")
	client := http.Client{}

	//POST body
	fmt.Println("(cancel_order) creating new POST request: ")
	request, err := http.NewRequest("POST", trade_request_url, bytes.NewBuffer(params))

	//set some headers
	fmt.Println("(cancel_order) setting request headers ...")
	request.Header.Set("Content-type", "application/json")
	request.Header.Set("requestToken", api_key)
	request.Header.Set("signature", sig)

	if err != nil {
		fmt.Println("(cancel_order) error after header addition: ", "(", orderIndex, ")")
		log.Fatalln(err)
	}

	fmt.Println("(cancel_order) executing the POST to ", "(", orderIndex, ")", trade_request_url)
	resp, err := client.Do(request)

	if err != nil {
		fmt.Println("(cancel_order) error after executing POST: ", "(", orderIndex, ")")
		log.Fatalln(err)

		//record as a failure metric
		recordFailedCancelMetrics()
	}

	defer resp.Body.Close()
	fmt.Println("(cancel_order) reading response body ...", "(", orderIndex, ")")
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(cancel_order) error reading response body: ", "(", orderIndex, ")")
		log.Fatalln(err)
	}

	sb := string(body)

	fmt.Println("(cancel_order) got response output: ", "(", orderIndex, ")", sb)

	//record this as a success metric
	recordSuccessCancelMetrics()

}

func cancelAllOrders(secret_key string, api_key string, base_url string, requestParameters map[string]string) {

	//Request body for POSTing a Trade
	params, err := json.Marshal(requestParameters)

	if err != nil {
		fmt.Println("(cancelAllOrders) failed to jsonify: ", params)
	}

	requestString := string(params)

	//debug
	fmt.Println("(cancelAllOrders) request parameters -> ", requestString)
	sig := sign_api_request(secret_key, requestString)

	//debug
	fmt.Println("(cancelAllOrders) request signature -> ", sig)
	trade_request_url := "https://" + base_url + "/api/cancelAll"

	//Set the client connection custom properties
	fmt.Println("(cancelAllOrders) setting client connection properties.")

	client := http.Client{}

	//POST body
	fmt.Println("(cancelAllOrders) creating new POST request: ")

	request, err := http.NewRequest("POST", trade_request_url, bytes.NewBuffer(params))
	//set some headers

	fmt.Println("(cancelAllOrders) setting request headers ...")

	request.Header.Set("Content-type", "application/json")
	request.Header.Set("requestToken", api_key)
	request.Header.Set("signature", sig)

	if err != nil {
		fmt.Println("(cancelAllOrders) error after header addition: ")
		log.Fatalln(err)
	}

	//Execute the post
	fmt.Println("(cancelAllOrders) executing the POST ...")

	resp, err := client.Do(request)

	if err != nil {
		fmt.Println("(cancelAllOrders) error after executing POST: ")
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	fmt.Println("(cancelAllOrders) reading response body ...")

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(cancelAllOrders) error reading response body: ")
		log.Fatalln(err)
	}

	sb := string(body)
	fmt.Println("(cancelAllOrders) got response output: ", sb)

}

//Instrumentation and metrics
var (
	consumedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_consumer_consumed_requests_total",
		Help: "The total number of requests taken from load queue",
	})

	requestsSuccessful = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_consumer_successul_requests_total",
		Help: "The total number of processed requests",
	})

	requestsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_consumer_failed_requests_total",
		Help: "The total number of failed requests",
	})

	requestsCancelSuccessful = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_cancel_successul_requests_total",
		Help: "The total number of processed order cancel requests",
	})

	requestsCancelFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_cancel_failed_requests_total",
		Help: "The total number of failed order cancel requests",
	})
)

func recordConsumedMetrics() {
	go func() {
		consumedRequests.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordSuccessMetrics() {
	go func() {
		requestsSuccessful.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordFailedMetrics() {
	go func() {
		requestsFailed.Inc()
		time.Sleep(2 * time.Second)

	}()
}

func recordSuccessCancelMetrics() {
	go func() {
		requestsCancelSuccessful.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordFailedCancelMetrics() {
	go func() {
		requestsCancelFailed.Inc()
		time.Sleep(2 * time.Second)

	}()
}

//destination directory is used for now to simulate the remote API
//messages consumed from kafka are dumped into the output-api shared folder.
var output_directory string = os.Getenv("OUTPUT_DIRECTORY_PATH") + "/" // "/data/output-api"

//skeleton for an error logging and handling function
func check_errors(e error, jobId int) {

	if e != nil {
		logMessage := "error " + e.Error() + "skipping over " + strconv.Itoa(jobId)
		logger(logFile, logMessage)
	}

}

//3. Modify JSON document with newuser ID and any other details that's needed to update old order data
//updateOrder(order, account, blockWaitAck, userId, clOrdId)
func updateOrder(order map[string]string, account int, blockWaitAck int, userId int, clOrdId string) (Order map[string]string) {

	//replace the userId with the currently active user
	//Order = order
	fmt.Println("(updateOrder): updating this map: ", order)

	//debug
	fmt.Println("(updateOrder): updating these fields: ", userId, clOrdId, blockWaitAck, account)

	order["clOrdId"] = clOrdId
	order["userId"] = strconv.Itoa(userId)
	order["blockWaitAck"] = strconv.Itoa(blockWaitAck)
	order["account"] = strconv.Itoa(account)

	//debug
	fmt.Println("(updateOrder) after updating map: ", order)

	return order
}

//custom parsing of JSON struct
//Expected format as read from Pulsar topic:
//[{ "Name":"newOrder","ID":"14","Time":"1644469469070529888","Data":"loader-c7dc569f-8bkql","Eventname":"transactionRequest"}]
func parseJSONmessage(theString string) map[string]string {

	var dMap map[string]string

	fmt.Println("(parseJSONmessage) before stripping:   BEGIN->", theString, "<-END")

	theString = strings.Trim(theString, "[")
	theString = strings.Trim(theString, "]")

	fmt.Println("(parseJSONmessage) after stripping:    BEGIN->", theString, "<-END")
	fmt.Println("(parseJSONmessage) before marshalling: BEGIN->", theString, "<-END")

	//data := Payload{}

	//marshalling issue
	json.Unmarshal([]byte(theString), &dMap)

	//fmt.Println("(parseJSONmessage) after marshalling: ", dMap)

	/*
		dMap["instrumentId"] = data.InstrumentId
		dMap["symbol"] = data.Symbol
		dMap["userId"] = data.UserId
		dMap["side"] = data.Side
		dMap["ordType"] = data.OrdType
		dMap["price"] = data.Price
		dMap["price_scale"] = data.Price_scale
		dMap["quantity"] = data.Quantity
		dMap["quantity_scale"] = data.Quantity_scale
		dMap["nonce"] = data.Nonce
		dMap["blockWaitAck"] = data.BlockWaitAck
		dMap["clOrdId"] = data.ClOrdId
	*/

	fmt.Println("(parseJSONmessage) after marshaling: ", dMap)

	return dMap
}

func empty_msg_check(message string) (err error) {

	if len(message) == 0 {

		return errors.New("empty message from message queue: " + message)

	}

	return nil
}

//simple illustrative data check for message (this is optional, really)
//Add all your pre-POST data checking here!
func data_check(message string) (err error) {

	dMap := parseJSONmessage(message)

	for k := range dMap {
		if k != "clOrdId" {
			if len(dMap[k]) > 0 {
				fmt.Println("(data_check) checking payload message field (ok): ", k, " -> ", dMap[k])
			} else {
				return errors.New("(data_check) empty field in message! ... " + k)
			}
		}
	}

	return nil
}

//Example of a pretask run before the main work function
func notify_job_start(workerId int, jobNum int) {

	logMessage := "worker" + strconv.Itoa(workerId) + "started  job" + strconv.Itoa(jobNum)
	logger(logFile, logMessage)

}

//example of a post task run after the main work function
func notify_job_finish(workerId int, jobNum int) {

	logMessage := "worker" + strconv.Itoa(workerId) + "finished job" + strconv.Itoa(jobNum)
	logger(logFile, logMessage)
}

func logger(logFile string, logMessage string) {

	now := time.Now()
	msgTimestamp := now.UnixNano()

	logMessage = strconv.FormatInt(msgTimestamp, 10) + " [host=" + hostname + "]" + logMessage + " " + logFile
	fmt.Println(logMessage)
}

//2. Expand JSON into POST body
func jsonToMap(theString string) map[string]string {

	dMap := make(map[string]string)
	data := Payload{}

	fmt.Println("(jsonToMap) before stripping:   BEGIN->", theString, "<-END")
	theString = strings.Trim(theString, "[")
	theString = strings.Trim(theString, "]")
	fmt.Println("(jsonToMap) after stripping:    BEGIN->", theString, "<-END")

	fmt.Println("(jsonToMap) (parseJSONmessage) before marshalling: ", theString)

	fmt.Println("(jsonToMap) (parseJSONmessage) before marshalling - convert to byte: ", []byte(theString))

	//The Problem is Here
	json.Unmarshal([]byte(theString), &data)

	fmt.Println("(jsonToMap) (parseJSONmessage) after marshalling: ", data)

	dMap["instrumentId"] = data.InstrumentId
	dMap["symbol"] = data.Symbol
	dMap["userId"] = data.UserId
	dMap["side"] = data.Side
	dMap["ordType"] = data.OrdType
	dMap["price"] = data.Price
	dMap["price_scale"] = data.Price_scale
	dMap["quantity"] = data.Quantity
	dMap["quantity_scale"] = data.Quantity_scale
	dMap["nonce"] = data.Nonce
	dMap["blockWaitAck"] = data.BlockWaitAck
	dMap["clOrdId"] = data.ClOrdId

	fmt.Printf("(jsonToMap) returning map %s\n", dMap)

	return dMap
}

func consume_payload_data(client pulsar.Client, topic string, id int, credentials map[string]string) {

	// initialize a new reader with the brokers and topic
	// the groupID identifies the consumer and prevents
	// it from receiving duplicate messages

	var orderIndex int = 0

	logMessage := "(consume_payload_data) worker " + strconv.Itoa(id) + " consuming from topic " + topic
	logger("(consume_payload_data)", logMessage)

	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: subscriptionName,
		Type:             pulsar.Shared,
	})

	if err != nil {
		log.Fatal(err)
	}

	defer consumer.Close()

	for {

		// the `Receive` method blocks until we receive the next event
		msg, err := consumer.Receive(context.Background())

		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("(consume_payload_data) Received message msgId: %#v -- content: '%s' (%s)\n",
			msg.ID(), string(msg.Payload()), strconv.Itoa(orderIndex))

		//message acknowledgment
		//Do we need this ? Under what conditions ?
		consumer.Ack(msg)

		message := string(msg.Payload())

		orderIndex++

		err = empty_msg_check(message)

		if err != nil {

			logMessage := "(consume_payload_data) ERROR: empty message (skipping): " + "(" + strconv.Itoa(orderIndex) + ") " + message
			logger("(consume_payload_data)", logMessage)

		} else {

			//record this as a metric
			recordConsumedMetrics()

			err = data_check(message)

			if err != nil {

				fmt.Println("(consume_payload_data) data check error: ", "(", orderIndex, ")", err)
				//incremement error metric
				errorCount += 1
				logMessage := "(consume_payload_data) Error Count: " + "(" + strconv.Itoa(orderIndex) + ")" + strconv.Itoa(errorCount)
				logger("(consume_payload_data)", logMessage)

			} else {

				//sign the body and create an order (order map[string]string )
				fmt.Println("(consume_payload_data) converting this json string to map: ", "(", orderIndex, ")", message)
				order := jsonToMap(message) //convert the json string to a map[string]string to access the order elements

				fmt.Println("(consume_payload_data) json converted map: ", "(", orderIndex, ")", order)

				order = updateOrder(order, account, blockWaitAck, userID, clOrdId)

				fmt.Println("(consume_payload_data) updated order details: ", "(", orderIndex, ")", order)

				userId, _ := strconv.Atoi(credentials["request_id"])

				create_order(credentials["secret_key"], credentials["api_key"], base_url, order, orderIndex, credentials["request_id"], userId)

			}

			//Do a bulk order cancellation every 100 orders:
			if orderIndex == cancelAllThreshold {

				fmt.Println("(consume_payload_data) cancelling all orders after ", orderIndex, " orders", " threshold: ", cancelAllThreshold)

				//please clean this up
				request_id, _ := strconv.Atoi(credentials["request_id"])
				userId := request_id
				requestParams := make(map[string]string)
				requestParams["userId"] = strconv.Itoa(userId)

				cancelAllOrders(credentials["secret_key"], credentials["api_key"], base_url, requestParams)

			}

		}
	}

}

func dumb_worker(id int, client pulsar.Client, credentials map[string]string) {

	for {
		consume_payload_data(client, topic0, id, credentials)
	}
}

func apiLogon(username string, password string, userID int, base_url string) (credentials map[string]string) {

	params := `{ "login":"` + username + `",  "password":"` + password + `",  "userId":"` + strconv.Itoa(userID) + `"}`

	fmt.Println("(apiLogon) logging in with credentials: ", params)

	responseBytes := []byte(params)
	responseBody := bytes.NewBuffer(responseBytes)

	credentials = make(map[string]string)

	fmt.Println(responseBody)
	resp, err := http.Post("https://"+base_url+"/api/logon", "application/json", responseBody)

	//Handle Error
	if err != nil {
		log.Fatalf("An Error Occured %v", err)
	}

	defer resp.Body.Close()

	//Read the response body
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Fatalln(err)
	}

	sb := string(body)

	fmt.Println("(apiLogon) : getting request response body: ", sb) //marshall into an authcredential struct
	loginData := authCredential{}
	json.Unmarshal([]byte(sb), &loginData)

	//extract tokens
	fmt.Println("(apiLogon) : getting request ID: ", strconv.Itoa(loginData.Id))
	credentials["request_id"] = strconv.Itoa(loginData.Id)

	fmt.Println("(apiLogon) : getting api_key: ", loginData.RequestToken)
	credentials["api_key"] = loginData.RequestToken

	fmt.Println("(apiLogon) : getting secret_key: ", loginData.RequestSecret)
	credentials["secret_key"] = loginData.RequestSecret

	return credentials

}

func getAllunused() (hKeys []string) {

	//1. connect in read mode to db 14
	//2. collect all indexes in a slice
	//3. Return the slice

	//Get from Redis
	connr, err := redis.Dial("tcp", redisReadConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := connr.Do("AUTH", redisAuthPass)

	if err != nil {
		panic(err)
	} else {
		fmt.Println("redis auth response: ", response)
	}

	defer connr.Close()

	//select correct DB (0)
	connr.Do("SELECT", credentialsDBindex)

	//GET ALL VALUES FROM DISCOVERED KEYS ONLY
	hKeys, err = redis.Strings(connr.Do("KEYS", "*"))

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("(getCredentialbyIndex) got keys: ", hKeys)

	return hKeys
}

func getCredentialbyIndex(idx int) (u string, p string, i int, s string) {

	//Get from Redis
	connr, err := redis.Dial("tcp", redisReadConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := connr.Do("AUTH", redisAuthPass)

	if err != nil {
		panic(err)
	} else {
		fmt.Println("(getCredentialbyIndex) redis auth response: ", response)
	}

	defer connr.Close()

	//select correct DB (0)
	connr.Do("SELECT", credentialsDBindex)

	//GET ALL VALUES FROM DISCOVERED KEYS ONLY
	data, err := redis.Strings(connr.Do("KEYS", "*"))

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("(getCredentialbyIndex) got keys: ", data)

	d, _ := readFromRedis(idx, connr)

	fmt.Println("(getCredentialbyIndex) raw: ", d)

	u = d["username"]

	fmt.Println("(getCredentialbyIndex) username ", u)

	p = d["password"]

	i, _ = strconv.Atoi(d["userid"])

	s = d["used"]

	fmt.Println("(getCredentialbyIndex) credential id ", idx, " has status: ", s)

	connr.Close()

	fmt.Println("(getCredentialbyIndex) returning 4 values: ", u, p, i, s)

	return u, p, i, s
}

func readFromRedis(input_id int, connr redis.Conn) (ds map[string]string, err error) {

	msgPayload := make(map[string]string)

	err = nil

	connr.Do("SELECT", credentialsDBindex)

	fmt.Println("(readFromRedis) getting index: ", input_id)

	values, err := redis.Values(connr.Do("HGETALL", input_id))

	if err != nil {
		fmt.Println([]byte(err.Error()))
	}

	fmt.Println("(readFromRedis) got this from HGETALL: ", values)

	p := User{}

	redis.ScanStruct(values, &p)

	fmt.Println("(readFromRedis) scanned into struct: ", p)

	username := p.Username
	password := p.Password
	userid := p.Userid
	used := p.Used

	if used == "0" {
		fmt.Println("(readFromRedis) unused: ", used)
	} else {
		fmt.Println("(readFromRedis) used: ", used)
	}

	msgPayload["username"] = username
	msgPayload["password"] = password
	msgPayload["userid"] = userid
	msgPayload["used"] = used

	fmt.Println("(readFromRedis) debug -> ", msgPayload, " <- debug")

	/*
		used = "1"                                          //marked as "used" ("read")
		setErr := updateCredentialParameter(input_id, used) //update the credential to indicate it has been retrieved and is likely in use.
		if setErr != nil {
			fmt.Println("(readFromRedis) WARNING: failed to update credential-in-use flag ...", setErr)
		}
	*/

	//get all the required data for the input id and return as json string
	return msgPayload, err

}

func lookupRandomCredentials() (u string, p string, i int) {
	//get the first available credential and mark as used
	//lookup the credentials in the credentials table
	indices := getAllunused() //get all unused entries in the table
	tryCount := 0             //how many lookup treis before finding a free credential?
	availableCredentials := len(indices)

	var selected int = 0
	var userid int = 0

	for idx := range indices {

		u, p, i, s := getCredentialbyIndex(idx)

		fmt.Println("(lookupRandomCredentials) got next credentials: ", "db index = ", idx, " u = ", u, " p = ", p, " i = ", i, " s = ", s)

		if s == "1" {

			fmt.Println("(lookupRandomCredentials) user id: ", i, " is USED -> ", s)

			tryCount++

		} else {
			fmt.Println("(lookupRandomCredentials) user id: ", i, " is FREE -> ", s)
			selected = idx

			//pick a random one and jump out of the loop
			username = u
			password = p
			userid = i

			fmt.Println("(lookupRandomCredentials) got currently unused credentials: ", "db index = ", idx, " u = ", username, " p = ", password, " i = ", userid, " s = ", s)
			break
		}

		//because eventually there won't be enough credentials to go around ...
		fmt.Println("(lookupRandomCredentials) current: ", tryCount, " max: ", availableCredentials)

		if tryCount == availableCredentials {

			fmt.Println("(lookupRandomCredentials) no credentials are unused, so re-using")
			selected = idx

			//pick a random one and jump out of the loop
			username = u
			password = p
			userid = i

			fmt.Println("(lookupRandomCredentials) re-using credentials: ", "db index = ", idx, " u = ", username, " p = ", password, " i = ", userid, " s = ", s)
			break

		}

	}

	//connect to the write master of the redis cluster
	connw, err := redis.Dial("tcp", redisWriteConnectionAddress)
	if err != nil {
		fmt.Println("(lookupRandomCredentials) redis connection response: ")
		//	log.Fatal(err)
	}
	// Now authenticate
	response, err := connw.Do("AUTH", redisAuthPass)

	if err != nil {
		fmt.Println("(lookupRandomCredentials) redis auth response: ", response)
		panic(err)
	} else {
		fmt.Println("(lookupRandomCredentials) redis auth response: ", response)
	}
	//Use defer to ensure the connection is always
	//properly closed before exiting the main() function.
	defer connw.Close()

	used := 1 //marked as "used" ("read")

	setErr := updateCredentialParameter(selected, used, connw) //update the credential to indicate it has been retrieved and is likely in use.

	if setErr != nil {
		fmt.Println("(lookupRandomCredentials) WARNING: failed to update credential-in-use flag ...", setErr)
	}

	fmt.Println("(lookupRandomCredentials) total available credentials: ", availableCredentials)
	fmt.Println("(lookupRandomCredentials) tries before finding free credential: ", tryCount)
	fmt.Println("(lookupRandomCredentials) returning credentials: ", username, password, userid)

	return username, password, userid
}

func updateCredentialParameter(credentialIndex int, usedStatus int, connw redis.Conn) (err error) {

	fmt.Println("(updateCredentialParameter) calling redis single field update with value: ", usedStatus)

	updateErr := update_parameter_to_redis(credentialIndex, usedStatus, connw)

	fmt.Println("(updateCredentialParameter) update error response:", updateErr)

	return updateErr
}

func update_parameter_to_redis(credentialIndex int, usedStatus int, connw redis.Conn) (err error) {

	fmt.Println("(update_parameter_to_redis) will update in redis: " + strconv.Itoa(credentialIndex))

	//select correct DB (0)
	fmt.Println("(update_parameter_to_redis) switching DB index")
	connw.Do("SELECT", credentialsDBindex)
	fmt.Println("(update_parameter_to_redis) setting required parameter value: ", usedStatus)

	//Testing.
	//used, _ := strconv.Atoi(usedStatus)

	_, err = connw.Do("HSET", strconv.Itoa(credentialIndex), "used", usedStatus)

	if err != nil {
		fmt.Println("(update_parameter_to_redis) WARNING: error updating entry in redis " + strconv.Itoa(credentialIndex))
	} else {
		fmt.Println("(update_parameter_to_redis) successfuly updated entry in redis " + strconv.Itoa(credentialIndex))
	}

	fmt.Println("(update_parameter_to_redis) returning error response.")

	return err

}

func main() {

	username, password, userID := lookupRandomCredentials()
	fmt.Println("(main) looked up credentials for login: ", username, password, userID)

	//login to the Trading API (assuming a single user for now)
	credentials := apiLogon(username, password, userID, base_url)

	//update old order data with unique, current information
	request_id, _ := strconv.Atoi(credentials["request_id"])
	userID = request_id
	account = request_id

	//Connect to Pulsar
	client, err := pulsar.NewClient(
		pulsar.ClientOptions{
			URL:               pulsarBrokerURL,
			OperationTimeout:  30 * time.Second,
			ConnectionTimeout: 30 * time.Second,
		})

	if err != nil {
		log.Fatalf("(main) Could not instantiate Pulsar client: %v", err)
	}

	defer client.Close()

	go func() {
		//metrics endpoint
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(port_specifier, nil)

		if err != nil {
			fmt.Println("(main) Could not start the metrics endpoint: ", err)
		} else {
			fmt.Println("(main) done setting up metrics endpoint: ")
		}

	}()

	//using a simple, single threaded loop - sequential consumption
	fmt.Println("(main) contemplating running main worker loop ...")
	dumb_worker(1, client, credentials)
	fmt.Println("(main) done running main worker loop ...")

}
