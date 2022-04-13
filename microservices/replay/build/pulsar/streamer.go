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

	"github.com/apache/pulsar/pulsar-client-go/pulsar"
	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//Redis data storage details
var redisAuthPass string = os.Getenv("REDIS_PASS")
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER")         // port for metrics service to listen on
var loadStatus string = "pending"
var namespace string = "pulsar" //namespace of the pulsar queueing service

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

func streamSequence(start int, stop int, reader pulsar.Reader, ctx context.Context) (m map[int]string) {

	// Listen on the topic for incoming messages
	outerCnt := 0
	cnt := 0
	var stream bool = false
	streamContent := make(map[int]string)

	for {

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

func loadSequenceData(streamContent map[int]string) (string, error) {

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

	for orderKey, orderData := range streamContent {

		//process to redis ...
		order := jsonToMap(orderData)

		fCnt, errCnt = write_order_to_redis(fCnt, errCnt, orderKey, order, conn)

		loadStatus = "(loadSequenceData) working"

	}

	loadStatus = "done"
	fmt.Println("loading status: ", loadStatus)

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

	//select correct DB (0)
	conn.Do("SELECT", 5)

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

	// Instantiate a Pulsar client
	client, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: "pulsar://pulsar-broker.pulsar.svc.cluster.local:6650",
	})

	if err != nil {
		log.Fatalf("Could not create client: %v", err)
	}

	// Use the client to instantiate a reader
	reader, err := client.CreateReader(pulsar.ReaderOptions{
		Topic:          "ragnarok/transactions/requests",
		StartMessageID: pulsar.EarliestMessage,
	})

	if err != nil {
		log.Fatalf("Could not create reader: %v", err)
	}

	ctx := context.Background()

	defer reader.Close()
	defer client.Close()

	//get and process the stream in one function
	streamContent := streamSequence(start, stop, reader, ctx)

	status, errorCount := loadSequenceData(streamContent)

	fmt.Println("loading status: ", status, errorCount)

	//We need to do this for now due to: https://github.com/apache/pulsar/issues/15045
	go func() {
		reloadQueueHack(namespace, w, r) //temporary  hack to restart pular. Remove once resolved.
	}()

	w.Write([]byte("<html> loading sequence status: " + status + " errors: " + string(errorCount.Error()) + "</html>"))

	status = errorCount.Error()

	w.Write([]byte("<html> loading sequence status: " + status + "</html>"))

}

func reloadQueueHack(namespace string, w http.ResponseWriter, r *http.Request) (status error) {

	// restart pulsar (kubectl rollout restart sts pulsar-broker -n pulsar)
	//scale down to 0, then scale up to the current max.
	fmt.Println("(reloadQueueHack) reloading pulsar ...")

	arg1 := "kubectl"
	arg2 := "rollout"
	arg3 := "statefulset"
	arg4 := "pulsar-broker"
	arg5 := "--replicas=0"
	arg6 := "--namespace"
	arg7 := namespace

	cmd := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)

	time.Sleep(5 * time.Second) //really should have a loop here waiting for returns ...

	out, err := cmd.Output()

	if err != nil {

		fmt.Println("(reloadQueueHack) ", err)
		return status

	}

	temp := strings.Split(string(out), "\n")
	theOutput := strings.Join(temp, `\n`)

	//for the user
	w.Write([]byte("<html> <br>restarted queue brokers: " + theOutput + "</html>"))

	time.Sleep(5 * time.Second)

	//check restart status (kubectl get pods -n pulsar -l component=broker) - loop and check for 1/1 on all rows

	arg1 = "kubectl"
	arg2 = "get"
	arg3 = "pods"
	arg4 = "--namespace"
	arg5 = namespace
	arg6 = "-l"
	arg7 = "component=broker"

	cmd = exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)

	time.Sleep(5 * time.Second) //really should have a loop here waiting for returns ...

	out, err = cmd.Output()

	if err != nil {

		fmt.Println("(reloadQueueHack) ", err)
		return status

	}

	temp = strings.Split(string(out), "\n")
	theOutput = strings.Join(temp, `\n`)

	//for the user
	w.Write([]byte("<html> <br>restarted queue brokers: " + theOutput + "</html>"))

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

		theThing(start, end, w, r)

	}

	if selection[parts[0]] == "LoadStreamStatus" {

		fmt.Println("loading stream sequence")
		w.Write([]byte("<html> getting stream sequence status </html>"))

		//Place streaming styatus function here

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

func (a adminPortal) initialiseHandler(w http.ResponseWriter, r *http.Request) {
	//don't block on this (the user can poll the status url for updates ...)
	go func() {

		fmt.Println("checking status of streaming ...")

	}()
}

func (a adminPortal) statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(loadStatus))
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

	//initiate data download
	http.HandleFunc("/streamer-start", admin.statusHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start http service endpoint for streamer service: ", err)
	}

}
