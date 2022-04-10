package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
		Name: "sequence_orders_ingested_total",
		Help: "The total number of sequenced orders ingested to redis",
	})

	inputSequenceOrdersLoadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sequence_orders_ingesterrors_total",
		Help: "The total number of sequenced orders failed to ingest to redis",
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

			log.Fatalf("Error reading from topic: %v", err)

		} else {

			outerCnt++

			if outerCnt == start {

				stream = true
				fmt.Println("starting to count: (outer) ", outerCnt)

			} else if outerCnt == stop {

				stream = false

				fmt.Println("stopping to count: (outer) ", outerCnt)
				fmt.Println("reached stream end. breaking out.")

				break
			}

			if stream {

				cnt++
				content := string(msg.Payload())

				streamContent[cnt] = content

			}

		}
	}

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

	//Connect to redis store to dump ingested order data
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

		loadStatus = "working"

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

func theThing() {

	//end of read stream
	start := 16
	stop := 32

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

	selection := make(map[string]string)
	params := r.URL.RawQuery

	parts := strings.Split(params, "=")
	selection[parts[0]] = parts[1]

	if selection[parts[0]] == "streamer" {
		fmt.Println("running stream sequence")

		theThing()

		/*if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + strconv.Itoa(status) + "</html>"))
		}*/
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

	//Basic API Auth Example
	//Disabled for Testing

	/*user, pass, ok := r.BasicAuth()
	if !ok || user != "admin" || pass != a.password {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("401 - unauthorized"))
		return
	}
	*/

	//don't block on this (the user can poll the status url for updates ...)
	go func() {

		theThing()
		/*
			if err != nil {
				w.Write([]byte(err.Error()))
			} else {
				w.Write([]byte(status))
			}
		*/

	}()

}

func (a adminPortal) statusHandler(w http.ResponseWriter, r *http.Request) {

	//Basic API Auth Example
	//Disabled for Testing

	/*user, pass, ok := r.BasicAuth()
	if !ok || user != "admin" || pass != a.password {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("401 - unauthorized"))
		return
	}
	*/

	w.Write([]byte(loadStatus))

}

func (a adminPortal) handler(w http.ResponseWriter, r *http.Request) {

	//Very Crude web-based option selection menu defined directly in html, no templates, no styles ...
	//<Insert Authentication Code Here>

	html_content := `
	<html><h1 style="font-family:verdana;">Trade Matching Load Data Ingestor</h1><br></html>
	<body>
	<div style="padding:10px;">
	<h3 style="font-family:verdana;">Select Load Ingestion Operations:</h3>
	  <br>

	  <form action="/ingestorselected?param=LoadHistoricalData" method="post">
	      <input type="submit" name="LoadHistoricalData" value="load from historical data" style="padding:20px;">
	      <br>
     </form>
	 
	 <form action="/ingestorselected?param=getLoadStatus" method="post" style="font-family:verdana;">
			<input type="submit" name="getLoadStatus" value="get download status" style="padding:20px;">
			<br>
	 </form>  

	 <form action="/ingestorselected?param=backupProcessedData" method="post" style="font-family:verdana;">
			<input type="submit" name="backupProcessedData" value="backup processed data" style="padding:20px;">
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
	http.HandleFunc("/streamer-start", admin.initialiseHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start http service endpoint for streamer service: ", err)
	}

}
