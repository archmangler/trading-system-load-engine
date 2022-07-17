package main

/* Consumer Pool Management Service*/

import (
	"bufio"
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

//Refresh FIX test configuration settings from configMap
var fixOrdersRate = os.Getenv("FIX_ORDERS_RATE")                              // FIX orders per second to the FIXAPI
var fixOrdersNewPercentage = os.Getenv("FIX_ORDERS_NEW_PERCENTAGE")           // percentage of new FIX orders in synthetic order load test (FIX API)
var fixOrdersMatchingPercentage = os.Getenv("FIX_ORDERS_MATCHING_PERCENTAGE") //
var fixOrdersCancelPercentage = os.Getenv("FIX_ORDERS_CANCEL")                //

var statsPrintingRate = os.Getenv("STATS_PRINTING_RATE")      //value: "5"
var fixOmTargetCompId = os.Getenv("FIX_OM_TARGET_COMPID")     //value: "testnet.fix-om.equos"
var fixOmHostIP = os.Getenv("FIX_OM_HOST_IP")                 //value: "10.0.43.159"
var fixOmHostPort = os.Getenv("FIX_OM_HOST_PORT")             //value: "4802"
var fixMdTargetCompId = os.Getenv("FIX_MD_TARGET_COMPID")     //value: "testnet.fix-om.equos"
var fixMdHostIp = os.Getenv("FIX_MD_HOST_IP")                 //value: "10.0.43.159"
var fixMdHostPort = os.Getenv("FIX_MD_HOST_PORT")             //value: 4802
var user1Username = os.Getenv("USER1_USERNAME")               //value: "test_eqonex_pt_22may16_indi_0lad@harakirimail.com"
var user1Password = os.Getenv("USER1_PASSWORD")               //value: "Diginextest@123"
var user1CompId = os.Getenv("USER1_COMPID")                   //value: "102283"
var configFilePath = os.Getenv("FIXTOOL_CONF_FILE")           // path to the fix testing util config file
var credentialFilePath = os.Getenv("FIXTOOL_CREDENTIAL_FILE") // path to the fix testing util user credential file
var toolJarPath = os.Getenv("FIXTOOL_JAR_PATH")
var instrumentFilePath = os.Getenv("FIXTOOL_INSTRUMENT_PATH")
var FIXTestMode = os.Getenv("FIXTOOL_TEST_MODE") //Supported tet scenarios are as follows: * log - log in the users and disconnect once all the users are logged in

//* ord - log in the users and place orders
//* mkt - log in the users and listen to market data
//* both - log in the users and place orders while listening to market da

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

var clOrdId string = os.Getenv("TRADING_API_CLORID")           //"test-1-traiano45"
var blockWaitAck, _ = strconv.Atoi(os.Getenv("BLOCKWAIT_ACK")) //blockwaitack

var batchIndex int = 0 //counter for batching up cancels
var cancelMap map[int]string
var cancelBatchLimit, _ = strconv.Atoi(os.Getenv("CANCEL_BATCH_LIMIT"))
var cancelAllThreshold, _ = strconv.Atoi(os.Getenv("CANCEL_ALL_THRESHOLD")) //cancelAllThreshold - run a cancellall after this many placed orders

//instrumentation variables
var datakeyMap = make(map[string]string)

//Metrics Instrumentation: Define the metrics here:
//1) map[OutgoingMessageRatespsCancel:0.0 OutgoingMessageRatespsNew:0.8 OutgoingMessageRatespsTotalMsgRate:0.8 OutgoingMessageRatespsTrades:0.0]
var (

	/*
	  .1          CancelOrderDelaysmsAvg=0,
	  .2          CancelOrderDelaysmsMin=0,
	  .3          CancelOrderDelaysmsMax=0
	*/

	CancelOrderdelaysms = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cancel_order_delays_ms_avg",
		Help: "Cancel Order Delays Milliseconds (average)",
	})

	IncomingmessagecountsExecReport = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_count_exec_report",
		Help: "Incoming Message Count Exec Report",
	})

	IncomingmessagecountsCancelReject = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_count_cancel_reject",
		Help: "Incoming Message Cancel Reject Count",
	})

	IncomingmessagecountsMDIncrUpdate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_count_md_incr_update",
		Help: "incoming message count MD Incr Update",
	})

	//IncomingMessageRatespsExecReport
	IncomingMessageRatespsExecReport = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_rates_persecond_exec_report",
		Help: "Incoming Message Rates persecond Exec Report",
	})

	IncomingMessageRatespsCancelReject = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_rates_persecond_cancel_reject",
		Help: "Incoming Message Rates per second Cancel Reject",
	})

	IncomingMessageRatespsMDIncrUpdate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_rates_ps_md_incr_update",
		Help: "incoming message rates per second MDIncrUpdate",
	})

	IncomingMessageRatespsmdIncrCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "incoming_message_rates_ps_mdincrcount",
		Help: "Incoming Message Rates ps mdIncrCount",
	})

	//outgoing messages metrics
	OutgoingMessageRatespsCancel = prometheus.NewGauge(prometheus.GaugeOpts{

		Name: "fix_outgoing_message_persecond_cancel",
		Help: "Outgoing messages cancelled per second",
	})

	OutgoingMessageRatespsNew = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_persecond_new",
		Help: "New Outgoing Messages per second",
	})

	OutgoingMessageRatespsTotalMsgRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_persecond_total",
		Help: "Outgoin FIX Message Rates ps Total Msg Rate",
	})

	OutgoingMessageRatespsTrades = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_rates_ps_trades",
		Help: "Outgoing FIX message rates per second trades",
	})

	//OutgoingMessageCounts  ->  map[OutgoingMessageCountsCancel:0 OutgoingMessageCountsNew:1725 OutgoingMessageCountsTotal:1725 OutgoingMessageCountsTrade:0]
	OutgoingMessageCountsCancel = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_counts_cancel",
		Help: "Outgoing FIX Message Counts Cancel",
	})

	OutgoingMessageCountsNew = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_counts_new",
		Help: "Outgoing FIX Message Counts New",
	})

	OutgoingMessageCountsTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_counts_total",
		Help: "Outgoing FIX Message Counts Total",
	})

	OutgoingMessageCountsTrade = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_outgoing_message_counts_trade",
		Help: "Outgoing FIX Message Counts Trade",
	})

	//new order delays
	NewOrderdelaysms = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "fix_new_order_delays_ms",
		Help: "New FIX Order delays average in milliseconds",
	})

	MDFullRefreshdelaysms = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "md_full_refresh_delays_ms",
		Help: "MD Full Refresh delays in average ms",
	})

	//MDIncrRefreshdelaysms  ->  map[MDIncrRefreshdelaysmsAvg:0 MDIncrRefreshdelaysmsMax:0 MDIncrRefreshdelaysmsMin:0]

	//For reference:
	/*
		datakeyMap["CancelOrder delays(ms)"] = "CancelOrderdelaysms"

		datakeyMap["Incoming Message Rates (per second)"] = "IncomingMessageRatesps"
		datakeyMap["Incoming message counts"] = "Incomingmessagecounts"

		datakeyMap["MDFullRefresh delays(ms)"] = "MDFullRefreshdelaysms"
		datakeyMap["MDIncrRefresh delays(ms)"] = "MDIncrRefreshdelaysms"

		datakeyMap["NewOrder delays(ms)"] = "NewOrderdelaysms"

		datakeyMap["Outgoing Message Counts"] = "OutgoingMessageCounts"
		datakeyMap["Outgoing Message Rates (per second)"] = "OutgoingMessageRatesps"
	*/

)

func initMetrics() {

	//MDFullRefreshdelaysms
	prometheus.MustRegister(MDFullRefreshdelaysms)

	//"NewOrderdelaysms"
	prometheus.MustRegister(CancelOrderdelaysms)

	//CancelOrderDelaysmsAvg
	prometheus.MustRegister(NewOrderdelaysms)

	// Metrics have to be registered to be exposed:
	prometheus.MustRegister(IncomingmessagecountsExecReport)
	prometheus.MustRegister(IncomingmessagecountsCancelReject)
	prometheus.MustRegister(IncomingmessagecountsMDIncrUpdate)

	prometheus.MustRegister(IncomingMessageRatespsExecReport)
	prometheus.MustRegister(IncomingMessageRatespsCancelReject)
	prometheus.MustRegister(IncomingMessageRatespsMDIncrUpdate)
	prometheus.MustRegister(IncomingMessageRatespsmdIncrCount)

	prometheus.MustRegister(OutgoingMessageRatespsCancel)
	prometheus.MustRegister(OutgoingMessageRatespsNew)
	prometheus.MustRegister(OutgoingMessageRatespsTotalMsgRate)
	prometheus.MustRegister(OutgoingMessageRatespsTrades)

	prometheus.MustRegister(OutgoingMessageCountsCancel)
	prometheus.MustRegister(OutgoingMessageCountsNew)
	prometheus.MustRegister(OutgoingMessageCountsTotal)
	prometheus.MustRegister(OutgoingMessageCountsTrade)

}

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

	fmt.Println("(create_order) input parameters before marshalling: ", orderParameters)

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
func updateOrder(order map[string]string, account string, blockWaitAck int, userId string, clOrdId string) (Order map[string]string) {

	//replace the userId with the currently active user
	//Order = order
	fmt.Println("(updateOrder): updating this map: ", order)

	//debug
	fmt.Println("(updateOrder): updating these fields: userid = ", userId, " clOrdId = ", clOrdId, " blockWaitAck = ", blockWaitAck, " account = ", account)

	order["clOrdId"] = clOrdId
	order["userId"] = userId
	order["blockWaitAck"] = strconv.Itoa(blockWaitAck)
	order["account"] = account

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

	fmt.Println("(parseJSONmessage) after marshalling: ", dMap)

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

func data_check(message string) (err error) {
	//simple illustrative data check for message (this is optional, really)
	//Add all your pre-POST data checking here!

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

				account := credentials["account"]
				userID := credentials["userid"]

				fmt.Println("(consume_payload_data) will update order with these parameters: account = ", account, " userID = ", userID)

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

	credentials["username"] = username
	credentials["password"] = password

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

	//Testing.
	used := strconv.Itoa(usedStatus)
	fmt.Println("(update_parameter_to_redis) setting required parameter value: ", usedStatus, " -> string -> ", used)

	_, err = connw.Do("HSET", strconv.Itoa(credentialIndex), "used", used)

	if err != nil {
		fmt.Println("(update_parameter_to_redis) WARNING: error updating entry in redis " + strconv.Itoa(credentialIndex))
	} else {
		fmt.Println("(update_parameter_to_redis) successfuly updated entry in redis " + strconv.Itoa(credentialIndex))
	}

	fmt.Println("(update_parameter_to_redis) returning error response.")

	return err

}

func buildFIXConf(credentials map[string]string) {

	//create fix performance testing tool configuration files
	//and populate with credentials and FIX GW parameters
	//1. Create `config.properties`
	//2. Create `users.csv` ... kind of redundant bu that's the way the tool works.

	fileMap := make(map[string]string)

	//update the default user credentials with a random unique lookup from the
	//db of synthetic users we have in redis

	user1Username = credentials["username"]
	user1Password = credentials["password"]
	user1CompId = credentials["account"]

	//crude, but effective ...
	fileMap["l1"] = "orders.rate=" + fixOrdersRate
	fileMap["l2"] = "orders.newPercentage=" + fixOrdersNewPercentage
	fileMap["l3"] = "orders.matchingPercentage=" + fixOrdersMatchingPercentage
	fileMap["l4"] = "orders.cancelPercentage=" + fixOrdersCancelPercentage
	fileMap["l5"] = "stats.rate=" + statsPrintingRate
	fileMap["l6"] = "env.fixOM.targetCompId=" + fixOmTargetCompId
	fileMap["l7"] = "env.fixOM.host=" + fixOmHostIP
	fileMap["l8"] = "env.fixOM.port=" + fixOmHostPort
	fileMap["l9"] = "env.fixMD.targetCompId=" + fixOmTargetCompId
	fileMap["l10"] = "env.fixMD.host=" + fixMdHostIp
	fileMap["l11"] = "env.fixMD.port=" + fixMdHostPort
	fileMap["l12"] = "user.1.username=" + user1Username
	fileMap["l13"] = "user.1.password=" + user1Password
	fileMap["l14"] = "user.1.compId=" + user1CompId

	//write to file
	f, err := os.Create(configFilePath)

	if err != nil {
		fmt.Println("(buildFIXConf) ", err)
	}

	defer f.Close()

	for line := range fileMap {
		fmt.Println("(buildFIXConf) print line to file: ", configFilePath, " -> ", fileMap[line])
		fmt.Fprintln(f, fileMap[line])
	}

	fmt.Println("(buildFIXConf) done writing: ", configFilePath)

	//write to file
	f, err = os.Create(credentialFilePath)

	if err != nil {
		fmt.Println("(buildFIXConf) failed to write user credential file: ", err)
	}

	defer f.Close()

	fileMap = make(map[string]string)

	fileMap["header"] = "id,username,password,cod,account,mode"
	fileMap["data1"] = user1CompId + "," + user1Username + "," + user1Password + "," + "true" + "," + user1CompId + "," + "3"

	for line := range fileMap {
		fmt.Println("(buildFIXConf) print line to file: ", credentialFilePath, " -> ", fileMap[line])
		fmt.Fprintln(f, fileMap[line])
	}

	fmt.Println("(buildFIXConf) done writing: ", credentialFilePath)

}

func executeFIXorderRequest(orderData map[string]string, mode string) (o map[int]string, e error) {

	// java -jar target/fix-client-1.0-SNAPSHOT.jar -c config/config.properties -u config/users.csv -i config/instruments.csv -t both
	resultText := ""

	arg1 := "java"
	arg2 := "-jar"
	arg3 := toolJarPath //"target/fix-client-1.0-SNAPSHOT.jar"
	arg4 := "-c"
	arg5 := configFilePath
	arg6 := "-u"
	arg7 := credentialFilePath
	arg8 := "-i"
	arg9 := instrumentFilePath
	arg10 := "-t"
	arg11 := mode

	cmd := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8, arg9, arg10, arg11)

	fmt.Println("(executeFIXorderRequest) BEGIN getting stdout from command ...")

	//stream command output continuously
	stdout, _ := cmd.StdoutPipe()
	cmd.Start()
	scanner := bufio.NewScanner(stdout)
	//scanner.Split(bufio.ScanWords)

	//for instrumentation
	lc := 0
	read := 0 //read flag
	dataBloc := make(map[int]string)

	//command content map ...
	outputData := make(map[int]string)

	for scanner.Scan() {

		m := scanner.Text()

		resultText = m //save the last output from the command ...

		fmt.Println(m)
		fmt.Println("(executeFIXorderRequest) -> (", lc, ") ", m)

		//return and retry the entire logon process on failure
		if strings.Contains(m, "UNABLE_TO_LOGON") {

			e = errors.New("logonfail")

			return o, e

		}

		outputData[lc] = m
		lc++

		//Begin instrumentation configuration: extract metrics and surface for prometheus
		if strings.Contains(m, "Stat Update") && read != 1 {

			read = 1
			fmt.Println("(executeFIXorderRequest) begin reading data bloc -> ", m)
			dataBloc[lc] = m

		}

		if read == 1 {

			dataBloc[lc] = m
			lc++

		}

		//when to stop reading the statistics printout from the FIX perf  tool
		if strings.Contains(m, "MDIncrRefresh") && read == 1 {

			read = 0
			fmt.Println("(executeFIXorderRequest) end reading data bloc -> ", m)

			dataBloc[lc] = m

			//dump the bloc
			fmt.Println("*** DUMP DATA BLOC *** ")

			for item := range dataBloc {
				fmt.Println(item, " => ", dataBloc[item])
				s := strings.Split(dataBloc[item], ":")

				dataKey := s[0]
				dataVal := s[1]

				fmt.Println("(executeFIXorderRequest) ", dataKey, " => ", dataVal)
				extractMetrics(dataKey, dataVal)

			}

			lc = 0

			dataBloc = make(map[int]string)

		}

		//end instrumentation configuration

		if lc >= 1000000 {
			fmt.Println("(executeFIXorderRequest) got enough feedback. stopping fix test ...")
			break
		}

		fmt.Println("(executeFIXorderRequest) still in loop")

	}

	fmt.Println("(executeFIXorderRequest) I have exited the command loop!")

	//cmd.Wait()

	//End of streaming
	fmt.Println("(executeFIXorderRequest) DONE getting stderr from command ...", resultText)

	return outputData, nil

}

func processData(o map[int]string) {
	for l := range o {
		fmt.Println("(processData) -> ", o[l])
	}
}

func extractMetrics(dataKey string, dataVal string) {

	metricMap, metricMapKey, e := getMetricHeader(dataKey, dataVal) //create the metric header

	if e != nil {

		fmt.Println("(extractMetrics) no metrics here.")

	} else {

		fmt.Println("(extractMetrics) will analyse dimension: ", metricMapKey, " -> ", metricMap)
		//update the metric counters using `metricMap`: update the golang metric counter function for each metric
		updateMetrics(metricMapKey, metricMap)
	}

}

func updateMetrics(metricMapKey string, metricMap map[string]string) {

	//update the intrumentation counters to surface metrics for golang
	fmt.Println("(updateMetrics) analysing dimension: ", metricMapKey, " -> ", metricMap)

	//For reference:
	/*
		datakeyMap["CancelOrder delays(ms)"] = "CancelOrderdelaysms"
		datakeyMap["Incoming Message Rates (per second)"] = "IncomingMessageRatesps"
		datakeyMap["Incoming message counts"] = "Incomingmessagecounts"
		datakeyMap["MDFullRefresh delays(ms)"] = "MDFullRefreshdelaysms"
		datakeyMap["MDIncrRefresh delays(ms)"] = "MDIncrRefreshdelaysms"
		datakeyMap["NewOrder delays(ms)"] = "NewOrderdelaysms"
		datakeyMap["Outgoing Message Counts"] = "OutgoingMessageCounts"
		datakeyMap["Outgoing Message Rates (per second)"] = "OutgoingMessageRatesps"
	*/

	fmt.Println("(updateMetrics) updating metrics class: ", metricMapKey, " -> ", metricMap)

	if metricMapKey == "CancelOrderdelaysms" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "CancelOrderdelaysms")

		//map[CancelOrderdelaysmsAvg:0 CancelOrderdelaysmsMax:0 CancelOrderdelaysmsMin:0]

		CancelOrderdelaysmsval := metricMap["CancelOrderdelaysmsAvg"]
		t, _ := strconv.ParseFloat(CancelOrderdelaysmsval, 32)

		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", CancelOrderdelaysmsval, " -> ", t)

		if s, err := strconv.ParseFloat(CancelOrderdelaysmsval, 32); err == nil {

			go func() {
				CancelOrderdelaysms.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: CancelOrderdelaysmsval -> ", CancelOrderdelaysmsval, err)
		}

	}

	if metricMapKey == "Incomingmessagecounts" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "Incomingmessagecounts")

		//map[IncomingmessagecountsCancelReject:0 IncomingmessagecountsExecReport:0 IncomingmessagecountsMDIncrUpdate:0]
		IncomingmessagecountsMDIncrUpdateval := metricMap["IncomingmessagecountsMDIncrUpdate"]
		IncomingmessagecountsExecReportval := metricMap["IncomingmessagecountsExecReport"]
		IncomingmessagecountsCancelRejectval := metricMap["IncomingmessagecountsCancelReject"]

		t, _ := strconv.ParseFloat(IncomingmessagecountsMDIncrUpdateval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingmessagecountsMDIncrUpdateval, " -> ", t)

		t, _ = strconv.ParseFloat(IncomingmessagecountsExecReportval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingmessagecountsExecReportval, " -> ", t)

		t, _ = strconv.ParseFloat(IncomingmessagecountsCancelRejectval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingmessagecountsCancelRejectval, " -> ", t)

		if s, err := strconv.ParseFloat(IncomingmessagecountsMDIncrUpdateval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingmessagecountsMDIncrUpdate -> ", s)

			go func() {
				IncomingmessagecountsMDIncrUpdate.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingmessagecountsMDIncrUpdate -> ", IncomingmessagecountsMDIncrUpdateval, err)
		}

		if s, err := strconv.ParseFloat(IncomingmessagecountsExecReportval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingmessagecountsExecReportval -> ", s)

			go func() {
				IncomingmessagecountsExecReport.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingmessagecountsExecReport -> ", IncomingmessagecountsExecReportval, err)
		}

		if s, err := strconv.ParseFloat(IncomingmessagecountsCancelRejectval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingmessagecountsCancelRejectval -> ", s)

			go func() {
				IncomingmessagecountsCancelReject.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingmessagecountsMDIncrUpdate -> ", IncomingmessagecountsMDIncrUpdateval, err)
		}

	}

	if metricMapKey == "IncomingMessageRatesps" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "IncomingMessageRatespsCancelReject")

		//map[IncomingMessageRatespsCancelReject:0.0 IncomingMessageRatespsExecReport:0.0 IncomingMessageRatespsMDIncrUpdate:0.0 IncomingMessageRatespsmdIncrCount:0]
		IncomingMessageRatespsCancelRejectval := metricMap["IncomingMessageRatespsCancelReject"]
		IncomingMessageRatespsExecReportval := metricMap["IncomingMessageRatespsExecReport"]
		IncomingMessageRatespsMDIncrUpdateval := metricMap["IncomingMessageRatespsMDIncrUpdate"]
		IncomingMessageRatespsmdIncrCountval := metricMap["IncomingMessageRatespsmdIncrCount"]

		t, _ := strconv.ParseFloat(IncomingMessageRatespsCancelRejectval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingMessageRatespsCancelRejectval, " -> ", t)
		t, _ = strconv.ParseFloat(IncomingMessageRatespsExecReportval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingMessageRatespsExecReportval, " -> ", t)
		t, _ = strconv.ParseFloat(IncomingMessageRatespsMDIncrUpdateval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingMessageRatespsMDIncrUpdateval, " -> ", t)
		t, _ = strconv.ParseFloat(IncomingMessageRatespsmdIncrCountval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", IncomingMessageRatespsmdIncrCountval, " -> ", t)

		if s, err := strconv.ParseFloat(IncomingMessageRatespsCancelRejectval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingMessageRatespsCancelReject -> ", s)

			go func() {
				IncomingMessageRatespsCancelReject.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingMessageRatespsCancelReject -> ", IncomingMessageRatespsCancelRejectval, err)
		}

		if s, err := strconv.ParseFloat(IncomingMessageRatespsExecReportval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingMessageRatespsExecReportval -> ", s)

			go func() {
				IncomingMessageRatespsExecReport.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingMessageRatespsExecReport -> ", IncomingMessageRatespsExecReportval, err)
		}

		if s, err := strconv.ParseFloat(IncomingMessageRatespsMDIncrUpdateval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingMessageRatespsMDIncrUpdateval -> ", s)

			go func() {
				IncomingMessageRatespsMDIncrUpdate.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingMessageRatespsMDIncrUpdate -> ", IncomingMessageRatespsMDIncrUpdateval, err)
		}

		if s, err := strconv.ParseFloat(IncomingMessageRatespsmdIncrCountval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting IncomingMessageRatespsmdIncrCountval -> ", s)

			go func() {
				IncomingMessageRatespsmdIncrCount.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: IncomingMessageRatespsmdIncrCountval -> ", IncomingMessageRatespsmdIncrCountval, err)
		}

	}

	if metricMapKey == "OutgoingMessageCounts" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "OutgoingMessageCounts")

		//OutgoingMessageCounts  ->  map[OutgoingMessageCountsCancel:0 OutgoingMessageCountsNew:1725 OutgoingMessageCountsTotal:1725 OutgoingMessageCountsTrade:0]

		OutgoingMessageCountsCancelval := metricMap["OutgoingMessageCountsCancel"]
		OutgoingMessageCountsNewval := metricMap["OutgoingMessageCountsNew"]
		OutgoingMessageCountsTotalval := metricMap["OutgoingMessageCountsTotal"]
		OutgoingMessageCountsTradeval := metricMap["OutgoingMessageCountsTrade"]

		t, _ := strconv.ParseFloat(OutgoingMessageCountsCancelval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageCountsCancelval, " -> ", t)

		t, _ = strconv.ParseFloat(OutgoingMessageCountsNewval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageCountsNewval, " -> ", t)

		t, _ = strconv.ParseFloat(OutgoingMessageCountsTotalval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageCountsTotalval, " -> ", t)

		t, _ = strconv.ParseFloat(OutgoingMessageCountsTradeval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageCountsTradeval, " -> ", t)

		if s, err := strconv.ParseFloat(OutgoingMessageCountsCancelval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageCountsCancelval -> ", s)

			go func() {
				OutgoingMessageCountsCancel.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageCountsCancel -> ", OutgoingMessageCountsCancelval, err)
		}

		if s, err := strconv.ParseFloat(OutgoingMessageCountsNewval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageCountsNewval -> ", s)

			go func() {
				OutgoingMessageCountsNew.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageCountsNew -> ", OutgoingMessageCountsNewval, err)
		}

		if s, err := strconv.ParseFloat(OutgoingMessageCountsTotalval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageCountsTotalval -> ", s)

			go func() {
				OutgoingMessageCountsTotal.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageCountsTotal -> ", OutgoingMessageCountsTotalval, err)
		}

		if s, err := strconv.ParseFloat(OutgoingMessageCountsTradeval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageCountsTradeval -> ", s)

			go func() {
				OutgoingMessageCountsTrade.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageCountsTrade -> ", OutgoingMessageCountsTradeval, err)
		}

	}

	if metricMapKey == "OutgoingMessageRatesps" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "OutgoingMessageRatesps")

		//map[OutgoingMessageRatespsCancel:0.0 OutgoingMessageRatespsNew:0.8 OutgoingMessageRatespsTotalMsgRate:0.8 OutgoingMessageRatespsTrades:0.0]
		OutgoingMessageRatespsCancelval := metricMap["OutgoingMessageRatespsCancel"]

		OutgoingMessageRatespsNewval := metricMap["OutgoingMessageRatespsNew"]
		OutgoingMessageRatespsTotalMsgRateval := metricMap["OutgoingMessageRatespsTotalMsgRate"]
		OutgoingMessageRatespsTradesval := metricMap["OutgoingMessageRatespsTrades"]

		t, _ := strconv.ParseFloat(OutgoingMessageRatespsCancelval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageRatespsCancelval, " -> ", t)
		t, _ = strconv.ParseFloat(OutgoingMessageRatespsNewval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageRatespsNewval, " -> ", t)
		t, _ = strconv.ParseFloat(OutgoingMessageRatespsTotalMsgRateval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageRatespsTotalMsgRateval, " -> ", t)
		t, _ = strconv.ParseFloat(OutgoingMessageRatespsTradesval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", OutgoingMessageRatespsTradesval, " -> ", t)

		if s, err := strconv.ParseFloat(OutgoingMessageRatespsCancelval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageRatespsCancel -> ", s)

			go func() {
				OutgoingMessageRatespsCancel.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageRatespsCancel -> ", OutgoingMessageRatespsCancelval, err)
		}

		if s, err := strconv.ParseFloat(OutgoingMessageRatespsNewval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageRatespsNewval -> ", s)

			go func() {
				OutgoingMessageRatespsNew.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageRatespsNew -> ", OutgoingMessageRatespsNewval, err)
		}

		if s, err := strconv.ParseFloat(OutgoingMessageRatespsTotalMsgRateval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageRatespsTotalMsgRateval -> ", s)

			go func() {
				OutgoingMessageRatespsTotalMsgRate.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageRatespsTotalMsgRate -> ", OutgoingMessageRatespsTotalMsgRateval, err)
		}

		//OutgoingMessageRatespsTradesval
		if s, err := strconv.ParseFloat(OutgoingMessageRatespsTradesval, 64); err == nil {

			fmt.Println("(updateMetrics) Setting OutgoingMessageRatespsTradesval -> ", s)

			go func() {
				OutgoingMessageRatespsTrades.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: OutgoingMessageRatespsTradesval -> ", OutgoingMessageRatespsTradesval, err)
		}

	}

	//NewOrderdelaysms
	if metricMapKey == "NewOrderdelaysms" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "NewOrderdelaysms")

		//NewOrderdelaysms  ->  map[NewOrderdelaysmsAvg:0 NewOrderdelaysmsMax:0 NewOrderdelaysmsMin:0]
		NewOrderdelaysmsval := metricMap["NewOrderdelaysmsAvg"]

		t, _ := strconv.ParseFloat(NewOrderdelaysmsval, 64)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", NewOrderdelaysmsval, " -> ", t)

		if s, err := strconv.ParseFloat(NewOrderdelaysmsval, 32); err == nil {

			fmt.Println("(updateMetrics) Setting NewOrderdelaysmsAvg -> ", s)

			go func() {
				NewOrderdelaysms.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: NewOrderdelaysmsAvg -> ", NewOrderdelaysmsval, err)
		}

	}

	//MDFullRefreshdelaysms
	if metricMapKey == "MDFullRefreshdelaysms" {
		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "MDFullRefreshdelaysms")
		//MDFullRefreshdelaysms  ->  map[MDFullRefreshdelaysmsAvg:0 MDFullRefreshdelaysmsMax:0 MDFullRefreshdelaysmsMin:0]
		MDFullRefreshdelaysmsval := metricMap["MDIncrRefreshdelaysmsAvg"]
		t, _ := strconv.ParseFloat(MDFullRefreshdelaysmsval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", MDFullRefreshdelaysmsval, " -> ", t)
		if s, err := strconv.ParseFloat(MDFullRefreshdelaysmsval, 32); err == nil {
			fmt.Println("(updateMetrics) Setting MDFullRefreshdelaysmsAvg -> ", s)

			go func() {
				MDFullRefreshdelaysms.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {

			fmt.Println("(updateMetrics) failed to convert metric: MDFullRefreshdelaysmsAvg -> ", MDFullRefreshdelaysmsval, err)

		}

	}

	//MDIncrRefreshdelaysms
	if metricMapKey == "MDIncrRefreshdelaysms" {

		fmt.Println("(updateMetrics) found dimension: ", metricMapKey, " == ", "MDIncrRefreshdelaysms")

		// MDIncrRefreshdelaysms  ->  map[MDIncrRefreshdelaysmsAvg:0 MDIncrRefreshdelaysmsMax:0 MDIncrRefreshdelaysmsMin:0]
		MDIncrRefreshdelaysmsval := metricMap["MDIncrRefreshdelaysmsAvg"]
		t, _ := strconv.ParseFloat(MDIncrRefreshdelaysmsval, 32)
		fmt.Println("(updateMetrics) converting ", metricMapKey, ": ", MDIncrRefreshdelaysmsval, " -> ", t)
		if s, err := strconv.ParseFloat(MDIncrRefreshdelaysmsval, 32); err == nil {
			fmt.Println("(updateMetrics) Setting MDIncrRefreshdelaysmsval -> ", s)

			go func() {
				MDFullRefreshdelaysms.Set(s)
				time.Sleep(2 * time.Second)
			}()

		} else {
			fmt.Println("(updateMetrics) failed to convert metric: MDIncrRefreshdelaysmsAvg -> ", MDIncrRefreshdelaysmsval, err)
		}

	}

}

func getMetricHeader(dataKey string, dataVal string) (metrics map[string]string, dk string, e error) {

	fmt.Println("(getMetricHeader) about to process: ", dataKey, " -> ", dataVal)

	if !strings.Contains(dataKey, "DEBUG") {

		metrics = make(map[string]string)
		dataKey = lookupDataKeyMapping(dataKey)
		//create the metric header
		dataValParts := strings.Split(dataVal, ",")
		for p := range dataValParts {
			metricParts := strings.Split(dataValParts[p], "=")
			if len(metricParts) == 2 {
				metricKey := dataKey + strings.TrimSpace(metricParts[0])
				//fmt.Println("(getMetricHeader) metric: ", metricKey, " = ", metricParts[1])
				metrics[metricKey] = metricParts[1]
			}
		}

		e = nil //be explicit

	} else {
		e = errors.New("nullmetrics")
	}

	//don't accept empty metrics
	if len(metrics) == 0 {

		fmt.Println("(getMetricHeader) zero metrics length: ", len(metrics))
		e = errors.New("nullmetrics")

	}

	return metrics, dataKey, e
}

func lookupDataKeyMapping(dimension string) (dk string) {

	//lookup the dimension name based on it's free text description from the eFIX tool output format
	//NOTE: Any change to the FIX perf tool output format will break this!!

	dimension = strings.TrimSpace(dimension)

	dk = datakeyMap[dimension]

	//fmt.Println("(lookupDataKeyMapping) looking up: ->", dimension, "<- got ", dk)

	return dk
}

func main() {

	username, password, userID := lookupRandomCredentials()

	fmt.Println("(main) looked up credentials for login: ", username, password, userID)

	//login to the Trading API (assuming a single user for now)
	credentials := apiLogon(username, password, userID, base_url)

	//update old order data with unique, current information
	request_id, _ := strconv.Atoi(credentials["request_id"])
	userID = request_id
	account := request_id

	credentials["userid"] = strconv.Itoa(userID)
	credentials["account"] = strconv.Itoa(account)

	fmt.Println("(main) running this worker with user ID: ", credentials["userid"], " and account number: ", credentials["account"], credentials["username"], credentials["password"])

	//instrumentation metric lookup map
	datakeyMap["Outgoing Message Rates (per second)"] = "OutgoingMessageRatesps"
	datakeyMap["Incoming Message Rates (per second)"] = "IncomingMessageRatesps"
	datakeyMap["CancelOrder delays(ms)"] = "CancelOrderdelaysms"
	datakeyMap["Incoming message counts"] = "Incomingmessagecounts"
	datakeyMap["MDFullRefresh delays(ms)"] = "MDFullRefreshdelaysms"
	datakeyMap["MDIncrRefresh delays(ms)"] = "MDIncrRefreshdelaysms"
	datakeyMap["NewOrder delays(ms)"] = "NewOrderdelaysms"
	datakeyMap["Outgoing Message Counts"] = "OutgoingMessageCounts"

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

	//register metric
	initMetrics()

	//Build FIX tool configuration files
	buildFIXConf(credentials)

	retry := 1

	orderData := make(map[string]string)

	for retry != 0 {

		//Test execute FIX tool
		outputData, e := executeFIXorderRequest(orderData, FIXTestMode)

		if strings.Contains(e.Error(), "logonfail") {

			fmt.Println("(main) logon failed. Trying again: ", e.Error())
			retry = 1

		} else {
			retry = 0
			fmt.Println("(main) logon succeeded: ", e.Error())

			//process the resulting statistics
			processData(outputData)

		}

	}

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

	//using a simple, single threaded loop - sequential consumption
	fmt.Println("(main) contemplating running main worker loop ...")
	dumb_worker(1, client, credentials)
	fmt.Println("(main) done running main worker loop ...")

}
