package main

import (
	"context"
	"encoding/json"
	"fmt"
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

//Redis data storage details
var redisAuthPass string = os.Getenv("REDIS_PASS")
var sequenceReplayDBindex, _ = strconv.Atoi(os.Getenv("SEQUENCE_REPLAY_DB")) //sequenceReplayDBindex
var backupDBindex, _ = strconv.Atoi(os.Getenv("SEQUENCE_BACKUP_DB"))         //backupDBindex

var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER")         // port for metrics service to listen on
var loadStatus string = "pending"
var namespace string = "pulsar" //namespace of the pulsar queueing service
var backupStatus string = "pending"
var restoreStatus string = "pending"

type Order struct {
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

//Management Portal Component
type adminPortal struct {
	password string
}

//Metrics Instrumentation
var (
	inputSequenceOrdersLoadTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sequence_orders_streamed_total",
		Help: "The total number of sequenced orders streamed to redis",
	})

	inputSequenceOrdersLoadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sequence_orders_streamerrors_total",
		Help: "The total number of sequenced orders failed to stream to redis",
	})
)

func recordSuccessMetrics() {
	go func() {
		inputSequenceOrdersLoadTotal.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordFailureMetrics() {
	go func() {
		inputSequenceOrdersLoadErrors.Inc()
		time.Sleep(2 * time.Second)
	}()
}

//func backupStream(reader pulsar.Reader, ctx context.Context) (m map[int]string) {
func backupStream() (m map[int]string) {

	// Listen on the topic for incoming messages
	cnt := 0
	streamContent := make(map[int]string)

	client, err := pulsar.NewClient(pulsar.ClientOptions{URL: "pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	reader, err := client.CreateReader(pulsar.ReaderOptions{
		Topic:          "ragnarok/transactions/requests",
		StartMessageID: pulsar.EarliestMessageID(),
	})
	if err != nil {
		log.Fatal(err)
	}

	defer reader.Close()

	backupStatus = "working"

	for reader.HasNext() {

		msg, err := reader.Next(context.Background())

		if err != nil {

			backupStatus = "failed"

			log.Fatalf("(backupStream) Error reading from topic: %v", err)

		} else {

			content := string(msg.Payload())

			cnt++

			streamContent[cnt] = content

		}

	}

	backupStatus = "done"

	fmt.Println("(backupStream) done streaming out orders: ", cnt)

	return streamContent
}

func streamSequence(start int, stop int, reader pulsar.Reader, ctx context.Context) (m map[int]string) {
	// Listen on the topic for incoming messages
	outerCnt := 0
	cnt := 0
	var stream bool = false

	streamContent := make(map[int]string)

	for reader.HasNext() {
		msg, err := reader.Next(ctx)
		if err != nil {

			log.Fatalf("(streamSequence) Error reading from topic: %v", err)

		} else {

			outerCnt++

			if outerCnt == start {

				stream = true
				fmt.Println("(streamSequence) starting to count: (outer) ", outerCnt)

			} else if outerCnt == stop {

				stream = false

				fmt.Println("(streamSequence) stopping to count: (outer) ", outerCnt)
				fmt.Println("(streamSequence) reached stream end. breaking out.")

				break
			}

			if stream {
				cnt++
				content := string(msg.Payload())
				streamContent[cnt] = content
			}
		}
	}

	fmt.Println("(streamSequence) done collecting sequence", outerCnt)

	return streamContent

}

func jsonToMap(theString string) map[string]string {

	dMap := make(map[string]string)
	data := Order{}

	fmt.Println("(jsonToMap) parsing order json string: ", theString)

	theString = strings.Trim(theString, "[")
	theString = strings.Trim(theString, "]")

	json.Unmarshal([]byte(theString), &data)

	fmt.Println("debug> ", data.InstrumentId, data.Symbol, data.UserId)

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

	fmt.Printf("(jsonToMap) post-marshalling to map: %s\n", dMap)

	return dMap
}

func loadSequenceData(streamContent map[int]string, dbIndex int) (string, error) {

	status := "ok"
	loadStatus := "pending"

	fCnt := 0
	errCnt := 0

	var err error

	//Connect to redis store to dump streamed order data
	conn, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		panic(err)
	} else {
		fmt.Println("redis auth response: ", response)
	}

	//Use defer to ensure the connection is always
	//properly closed before exiting the main() function.
	defer conn.Close()

	//select correct DB sequenceReplayDBindex
	result, err := conn.Do("SELECT", dbIndex)

	if err != nil {

		fmt.Println("(write_order_to_redis) failed to select redis db: ", dbIndex, err.Error())

	} else {

		fmt.Println("(write_order_to_redis) selecting redis db", dbIndex, result)

	}

	for orderKey, orderData := range streamContent {

		//process to redis ...
		order := jsonToMap(orderData)

		fCnt, errCnt = write_order_to_redis(fCnt, errCnt, orderKey, order, conn)

		loadStatus = "working"

	}

	loadStatus = "done"

	fmt.Println("(loadSequenceData) loading status: ", loadStatus)

	//report done
	return status, err

}

func write_order_to_redis(msgCount int, errCount int, msgIndex int, d map[string]string, conn redis.Conn) (int, int) {

	InstrumentId := d["instrumentId"]
	Symbol := d["symbol"]
	UserId := d["userId"]
	Side := d["side"]
	OrdType := d["ordType"]
	Price := d["price"]
	Price_scale := d["price_scale"]
	Quantity := d["quantity"]
	Quantity_scale := d["quantity_scale"]
	Nonce := d["nonce"]
	BlockWaitAck := "1" // force to 1 or d["blockWaitAck"]
	ClOrdId := d["clOrdId"]

	loadStatus = "working"

	//REDIFY
	fmt.Println("inserting : ", InstrumentId, Symbol, UserId, Side, OrdType, Price, Price_scale, Quantity, Quantity_scale, Nonce, BlockWaitAck, ClOrdId)

	_, err := conn.Do("HMSET", msgIndex, "instrumentId", InstrumentId, "symbol", Symbol, "userId", UserId, "side", Side, "ordType", OrdType, "price", Price, "price_scale", Price_scale, "quantity", Quantity, "quantity_scale", Quantity_scale, "nonce", Nonce, "blockWaitAck", BlockWaitAck, "clOrdId", ClOrdId)

	if err != nil {

		recordFailureMetrics()
		errCount++

	} else {

		recordSuccessMetrics()
		msgCount++
	}

	return msgCount, errCount
}

func theThing(start int, stop int, w http.ResponseWriter, r *http.Request) {

	loadStatus = "reloading"

	client, err := pulsar.NewClient(pulsar.ClientOptions{URL: "pulsar://pulsar-broker.pulsar.svc.cluster.local:6650"})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	reader, err := client.CreateReader(pulsar.ReaderOptions{
		Topic:          "ragnarok/transactions/requests",
		StartMessageID: pulsar.EarliestMessageID(),
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	defer reader.Close()

	//get and process the stream in one function
	streamContent := streamSequence(start, stop, reader, ctx)

	status, errorCount := loadSequenceData(streamContent, sequenceReplayDBindex)

	fmt.Println("loading status: ", status, errorCount)
	loadStatus = "reloaded"

	w.Write([]byte("<html> loading sequence status: " + status + " errors: " + string(errorCount.Error()) + "</html>"))

	status = errorCount.Error()

	w.Write([]byte("<html> loading sequence status: " + status + "</html>"))

}

func reloadQueueHack(namespace string, ls string, w http.ResponseWriter, r *http.Request) (status error) {

	// restart pulsar (kubectl rollout restart sts pulsar-broker -n pulsar)
	//scale down to 0, then scale up to the current max.
	loadStatus = ls

	fmt.Println("(reloadQueueHack) reloading pulsar ...")

	arg1 := "kubectl"
	arg2 := "rollout"
	arg3 := "restart"
	arg4 := "sts"
	arg5 := "pulsar-broker"
	arg6 := "--namespace"
	arg7 := namespace

	fmt.Println("(reloadQueueHack) reloading with : " + arg1 + " " + arg2 + " " + arg3 + " " + arg4 + " " + arg5 + " " + arg6 + " " + arg7)
	cmd := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)

	out, err := cmd.Output()

	//debugging
	fmt.Println("(reloadQueueHack) reload output: ", out, " errors: ", err)

	if err != nil {
		fmt.Println("(reloadQueueHack) ", err)
		return status
	} else {
		fmt.Println("(reloadQueueHack) reload OK")
	}

	temp := strings.Split(string(out), "\n")

	for row := range temp {

		fmt.Println("(reloadQueueHack) brokers status: ", temp[row])

		w.Write([]byte("<html> <br>brokers status: " + temp[row] + "</html>"))

	}

	for {

		//check restart status (kubectl get pods -n pulsar -l component=broker) - loop and check for 1/1 on all rows
		//kubectl rollout status sts pulsar-broker --namespace pulsar
		arg1 = "kubectl"
		arg2 = "rollout"
		arg3 = "status"
		arg4 = "--namespace"
		arg5 = namespace
		arg6 = "sts"
		arg7 = "pulsar-broker"

		cmd = exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)

		out, err = cmd.Output()

		if err != nil {
			fmt.Println("(reloadQueueHack) ", err)
			return status
		} else {
			fmt.Println("(reloadQueueHAck) Check reload status - OK")
		}

		if strings.Contains(string(out), "statefulset rolling update complete") {

			fmt.Println("(reloadQueueHack) brokers status: ", string(out))
			w.Write([]byte("<html> <br>broker status: " + string(out) + "</html>"))

			loadStatus = "reloaded"

			break

		} else {
			fmt.Println("(reloadQueueHack) checking brokers status: ")
		}

	}

	fmt.Println("(reloadQueueHack) done reloading checks: err = ", err)

	return err
}

func newAdminPortal() *adminPortal {

	//initialise the management portal with a loose requirement for username:password
	password := os.Getenv("ADMIN_PASSWORD")

	if password == "" {
		panic("required env var ADMIN_PASSWORD not set")
	}

	return &adminPortal{password: password}
}

func (a adminPortal) selectionHandler(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	fmt.Println("done parsing form")

	selection := make(map[string]string)

	params := r.URL.RawQuery

	parts := strings.Split(params, "=")

	w.Write([]byte("<html> got input parts ->" + string(parts[0]) + " </html>"))
	w.Write([]byte("<html> got input parts ->" + string(parts[1]) + " </html>"))

	selection[parts[0]] = parts[1]

	if selection[parts[0]] == "LoadStreamSequence" {

		start_of_sequence := strings.Join(r.Form["start"], " ")
		end_of_sequence := strings.Join(r.Form["stop"], " ")

		fmt.Println("running stream sequence with stop = " + string(end_of_sequence) + " and start = " + string(start_of_sequence))
		w.Write([]byte("<html> running stream sequence with stop = " + string(end_of_sequence) + " and start = " + string(start_of_sequence) + "</html>"))

		start, _ := strconv.Atoi(start_of_sequence)
		end, _ := strconv.Atoi(end_of_sequence)

		fmt.Println("reading from "+start_of_sequence+" to "+end_of_sequence, start, end)

		theThing(start, end, w, r)

	}

	if selection[parts[0]] == "LoadStreamStatus" {

		fmt.Println("loading stream sequence")
		w.Write([]byte("<html> getting stream sequence status </html>"))

		//Place streaming status function here
		w.Write([]byte(loadStatus))

	}

	html_content := `
	<body>
	<br>
    <form action="/streamer-admin" method="get">
		   <input type="submit" name="back" value="back to main page">
	</form>
	</body>
	`
	w.Write([]byte(html_content))
}

func (a adminPortal) backupHandler(w http.ResponseWriter, r *http.Request) {

	backupStatus = "pending"

	fmt.Println("(backupHandler) user requested backup of load queue")
	w.Write([]byte(backupStatus))

	//stream all messages in order out of Pulsar/Kafka
	streamContent := backupStream()

	//load to redis (backup db 7)
	loadSequenceData(streamContent, backupDBindex)

	for order := range streamContent {
		fmt.Println("(backupHandler)", streamContent[order])
	}

	w.Write([]byte(backupStatus))

}

func (a adminPortal) restoreHandler(w http.ResponseWriter, r *http.Request) {

	restoreStatus = "pending"
	fmt.Println("(restoreHandler) user requested restore of load queue")
	w.Write([]byte(restoreStatus))

	//insert magic here
	//1. [] retrieve from redis db7
	//2. [] stream to pulsar (could take a long time)

	restoreStatus = "done"
	w.Write([]byte(restoreStatus))

}

func (a adminPortal) statusHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("(statusHandler) user requested load status")
	w.Write([]byte(loadStatus))
}

func (a adminPortal) refreshHandler(w http.ResponseWriter, r *http.Request) {

	fmt.Println("(refreshHandler) user requested streaming queue reload. This will take a while ...")

	loadStatus = "refreshing"

	w.Write([]byte(loadStatus))

	//don't bloc on this
	go func() {

		reloadQueueHack(namespace, loadStatus, w, r)

	}()

	for i := 0; i < 20; i++ {

		w.Write([]byte(loadStatus))

		fmt.Println("(refreshHandler) refreshing pulsar")

		time.Sleep(2 * time.Second)

	}

	fmt.Println("(refreshHandler) done reloading pulsar streaming system.")
}

func (a adminPortal) loadHandler(w http.ResponseWriter, r *http.Request) {

	fmt.Println("(loadHandler) user requested specified sequence")
	w.Write([]byte(loadStatus))

	params := r.URL.RawQuery
	fmt.Println("(loadHandler) parameter list (params): ", params)
	parts := strings.Split(params, "=")

	partsString := strings.Join(parts, " ")

	fmt.Println("(loadHandler) parameter list: ", partsString)

	//mangling out the parametes
	parameterParts := strings.Split(partsString, "?")

	startParams := parameterParts[0]
	fmt.Println("(loadHandler) parameterParts[0]: ", startParams)

	stopParams := parameterParts[1]
	fmt.Println("(loadHandler) parameterParts[1]: ", stopParams)

	start_of_sequence := strings.Split(startParams, " ")[1]
	end_of_sequence := strings.Split(stopParams, " ")[1]

	fmt.Println("(loadHandler) start, stop params: ", start_of_sequence, end_of_sequence)
	fmt.Println("(loadHandler) running stream sequence with stop = " + string(end_of_sequence) + " and start = " + string(start_of_sequence))

	fmt.Println("(loadHandler) reading from " + start_of_sequence + " to " + end_of_sequence)

	sos, err_sos := strconv.Atoi(start_of_sequence)
	eos, err_eos := strconv.Atoi(end_of_sequence)

	fmt.Println("type conversion debug: ", sos, eos, err_sos, err_eos)

	theThing(sos, eos, w, r)

}

func (a adminPortal) handler(w http.ResponseWriter, r *http.Request) {

	//Very Crude web-based option selection menu defined directly in html, no templates, no styles ...
	//<Insert Authentication Code Here>

	html_content := `
	<html><h1 style="font-family:verdana;">Trade Matching System Load Test Data Replay Service</h1><br></html>
	<body>
	<div style="padding:10px;">
	<h3 style="font-family:verdana;">Select Stream Replay Operations:</h3>
	  <br>

	 <form action="/streamerselected?param=LoadStreamSequence" method="post"">
	 <input type="submit" name="LoadStreamSequence" value="load request sequence" style="padding:20px;" style="font-family:verdana;"> 
 
	 <html style="font-family:verdana;">Start of sequence:</html><input type="text" name="start" >
	 <html style="font-family:verdana;">End of Sequence:</html><input type="text" name="stop" >

	 <form action="/streamerselected?param=LoadStreamStatus" method="post">
	      <input type="submit" name="LoadStreamStatus" value="get streaming status" style="padding:20px;">
	      <br>
     </form>
	 
	 </form>

</div>
	</body>
	`
	w.Write([]byte(html_content))
}

func main() {

	//set up the metrics and management endpoint
	//Prometheus metrics UI
	http.Handle("/metrics", promhttp.Handler())

	//Management UI for Load Data Management
	//Administrative Web Interface
	admin := newAdminPortal()
	http.HandleFunc("/streamer-admin", admin.handler)
	http.HandleFunc("/streamerselected", admin.selectionHandler)

	//read the load status data (statusHandler)
	http.HandleFunc("/streamer-status", admin.statusHandler)

	//initiate data load in sequence
	http.HandleFunc("/streamer-start", admin.loadHandler)

	//initiate pulsar/kafka queue backup to redis (in sequence)
	http.HandleFunc("/streamer-backup", admin.backupHandler)

	//initiate pulsar/kafka restore from redis (in sequence)
	http.HandleFunc("/streamer-restore", admin.restoreHandler)

	//reload the pulsar streaming system using refreshHandler
	http.HandleFunc("/streamer-refresh", admin.refreshHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start http service endpoint for streamer service: ", err)
	}

}
