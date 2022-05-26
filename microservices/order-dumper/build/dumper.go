package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//get running parameters from container environment
var sourceTopic string = os.Getenv("KAFKA_SOURCE_TOPIC")                //main  topic for inbound/historical  orders
var kafkaBroker string = os.Getenv("KAFKA_BROKER_SERVICE_ENDPOINT")     //kafka broker from order environment
var s3bucketAddress string = os.Getenv("S3_BUCKET_ADDRESS")             //"s3://tradedatasource/inputs"
var remoteSourceDir string = os.Getenv("REMOTE_SOURCE_DIRECTORY") + "/" //path under s3/efs/afs share: e.g "/inputs" (leading slash, no trailing slash)
var orderDumpDBIndex = os.Getenv("ORDER_DUMP_DB_INDEX")                 //ORDER_DUMP_DB_INDEX , index of  the REDIS  db to dump orders in

//Redis data storage details
var redisAuthPass string = os.Getenv("REDIS_PASS")
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER")         // port for metrics service to listen on
var loadStatus string = "pending"                                          //pending|working|done

//{"instrumentId":128,"symbol":"BTC/USDC[Infi]","userId":25097,"side":2,"ordType":2,"price":5173054,"price_scale":2,"quantity":61300,"quantity_scale":6,"nonce":1645495020701,"blockWaitAck":0,"clOrdId":""}
type Order struct {
	InstrumentId   int    `json:"instrumentId"`
	Symbol         string `json:"symbol"`
	UserId         int    `json:"userId"`
	Side           int    `json:"side"`
	OrdType        int    `json:"ordType"`
	Price          int    `json:"price"`
	Price_scale    int    `json:"price_scale"`
	Quantity       int    `json:"quantity"`
	Quantity_scale int    `json:"quantity_scale"`
	Nonce          int    `json:"nonce"`
	BlockWaitAck   int    `json:"blockWaitAck "`
	ClOrdId        string `json:"clOrdId"`
}

//Management Portal Component
type adminPortal struct {
	password string
}

//Metrics Instrumentation
var (
	inputOrdersLoadTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_orders_ingested_total",
		Help: "The total number of orders ingested to redis",
	})

	inputOrdersLoadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_orders_ingesterrors_total",
		Help: "The total number of orders failed to ingest to redis",
	})
)

func recordSuccessMetrics() {
	go func() {
		inputOrdersLoadTotal.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordFailureMetrics() {
	go func() {
		inputOrdersLoadErrors.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func newAdminPortal() *adminPortal {

	fmt.Println("(debug) creating the admin portal")

	//initialise the management portal with a loose requirement for username:password
	password := os.Getenv("ADMIN_PASSWORD")

	if password == "" {
		panic("required env var ADMIN_PASSWORD not set")
	}

	return &adminPortal{password: password}
}

func logger(logFile string, logMessage string) {
	now := time.Now()
	msgTs := now.UnixNano()

	//when we're not logging to file  ...
	fmt.Println("[log]", logFile, strconv.FormatInt(msgTs, 10), logMessage)

}

func backupProcessedData(w http.ResponseWriter, r *http.Request) (fcount int, err error) {
	//TBI
	fcount = 0

	//return count of files backed up
	return fcount, err
}

func jsonToMap(theString string) map[string]string {

	dMap := make(map[string]string)
	data := Order{}

	json.Unmarshal([]byte(theString), &data)

	dMap["instrumentId"] = fmt.Sprint(data.InstrumentId)
	dMap["symbol"] = string(data.Symbol)
	dMap["userId"] = fmt.Sprint(data.UserId)
	dMap["side"] = fmt.Sprint(data.Side)
	dMap["ordType"] = fmt.Sprint(data.OrdType)
	dMap["price"] = fmt.Sprint(data.Price)
	dMap["price_scale"] = fmt.Sprint(data.Price_scale)
	dMap["quantity"] = fmt.Sprint(data.Quantity)
	dMap["quantity_scale"] = fmt.Sprint(data.Quantity_scale)
	dMap["nonce"] = fmt.Sprint(data.Nonce)
	dMap["blockWaitAck"] = fmt.Sprint(data.BlockWaitAck)
	dMap["clOrdId"] = string(data.ClOrdId)

	fmt.Printf("debug %s\n", dMap)

	return dMap
}

func getOrderStream(sourceTopic string, kafkaBroker string) []string {

	//Dump orders from kafka using kafka tool
	//kafka-dump-tool-0.0.1-SNAPSHOT/bin/run.sh  --kafka-server=kafka1.dexp-qa.internal --kafka-port=4455 -q INPUT --topic=me0001 --output-file=in-msg.txt --start-time=2022-03-26T00:53:21.000Z --end-time=2022-03-26T01:02:24.000Z

	s := make([]string, 0)

	fmt.Println("(getOrderStream) getting order stream from kafka broker ...", kafkaBroker)

	arg1 := "kafka-dump-tool-0.0.1-SNAPSHOT/bin/run.sh"
	arg2 := "--kafka-server=kafka1.dexp-qa.internal"
	arg3 := "--kafka-port=4455"
	arg4 := "-q INPUT"
	arg5 := "--topic=me0001"
	arg6 := "--output-file=in-msg.txt"
	arg7 := "--start-time=2022-03-26T00:53:21.000Z"
	arg8 := "--end-time=2022-03-26T01:02:24.000Z"

	out, err := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8).Output()
	temp := strings.Split(string(out), "\n")

	if err != nil {

		fmt.Println("kafka dumper error! ", err)
		fmt.Println("kafka dumper output: ", temp)

	} else {

		fmt.Println("Done dumping orders ...", out)

	}
	//fill this with results
	return s

}

func getMessageContent(oName string) (content []string, err error) {

	return content, err

}

func loadHistoricalData(sourceTopic string, w http.ResponseWriter, r *http.Request) (string, error) {

	status := "ok"
	loadStatus = "started"
	var err error
	oCnt := 0
	errCnt := 0

	w.Write([]byte("<br><html>Using data in " + sourceTopic + " for new load test: " + "</html>"))
	logger("loadHistoricalData", "Using data in "+sourceTopic+" for new load test")

	//get available files from source bucket
	orderList := getOrderStream(sourceTopic, kafkaBroker) //stream messages out from Kafka broker endpoint

	/*

		DO  stuff here to process the rtetrieved order stream

	*/

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

	for o := range orderList {

		oName := string(orderList[o]) //hope this works out ...

		fmt.Println("reading out order: " + oName)

		content, err := getMessageContent(oName) //extract message content for insertion into redis

		if err != nil {

			fmt.Println("couldn't get message contents for ", o)

		} else {

			oData := strings.Join(content, "")

			order := jsonToMap(oData)

			fmt.Println("debug (order data): ", order)

			//process to redis ...
			oCnt, errCnt = write_binary_message_to_redis(oCnt, errCnt, o, order, conn)

			loadStatus = "working"

		}
	}

	loadStatus = "done"

	//report done
	return status, err

}

func (a adminPortal) selectionHandler(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	/* //Can debug as follows:
	for k, v := range r.Form {
		fmt.Println("key:", k)
		fmt.Println("val:", strings.Join(v, ""))
	}
	*/

	selection := make(map[string]string)
	params := r.URL.RawQuery

	parts := strings.Split(params, "=")
	selection[parts[0]] = parts[1]

	if selection[parts[0]] == "backupProcessedData" {
		fmt.Println("running backups")
		status, err := backupProcessedData(w, r)

		if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + strconv.Itoa(status) + "</html>"))
		}
	}

	if selection[parts[0]] == "LoadHistoricalData" {
		status, err := loadHistoricalData(sourceTopic, w, r)

		if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + status + "</html>"))
		}
	}

	html_content := `
	<body>
	<br>
    <form action="/dumper-admin" method="get">
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

		status, err := loadHistoricalData(sourceTopic, w, r)

		if err != nil {
			w.Write([]byte(err.Error()))
		} else {
			w.Write([]byte(status))
		}
	}()

}

func (a adminPortal) statusHandler(w http.ResponseWriter, r *http.Request) {

	fmt.Println("(debug) Accessing admin portal status handler.")

	w.Write([]byte(loadStatus))

}

func (a adminPortal) handler(w http.ResponseWriter, r *http.Request) {

	fmt.Println("(debug) Accessing admin portal.")

	//Very Crude web-based option selection menu defined directly in html, no templates, no styles ...
	//<Insert Authentication Code Here>

	html_content := `
	<html><h1 style="font-family:verdana;">Trade Matching Load Data dumper</h1><br></html>
	<body>
	<div style="padding:10px;">
	<h3 style="font-family:verdana;">Select Load Ingestion Operations:</h3>
	  <br>

	  <form action="/dumperselected?param=LoadHistoricalData" method="post">
	      <input type="submit" name="LoadHistoricalData" value="load from historical data" style="padding:20px;">
	      <br>
     </form>
	 
	 <form action="/dumperselected?param=getLoadStatus" method="post" style="font-family:verdana;">
			<input type="submit" name="getLoadStatus" value="get download status" style="padding:20px;">
			<br>
	 </form>  

	 <form action="/dumperselected?param=backupProcessedData" method="post" style="font-family:verdana;">
			<input type="submit" name="backupProcessedData" value="backup processed data" style="padding:20px;">
			<br>
	 </form>  



	</form>
</div>
	</body>
	`
	w.Write([]byte(html_content))
}

func write_binary_message_to_redis(msgCount int, errCount int, msgIndex int, d map[string]string, conn redis.Conn) (int, int) {

	logger("write_binary_message_to_redis", "#debug (write_binary_message_to_redis): "+strconv.Itoa(msgIndex))

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
	BlockWaitAck := d["blockWaitAck"]
	ClOrdId := d["clOrdId"]

	//select correct DB (0)
	conn.Do("SELECT", orderDumpDBIndex)

	//REDIFY
	fmt.Println("inserting : ", InstrumentId, Symbol, UserId, Side, OrdType, Price, Price_scale, Quantity, Quantity_scale, Nonce, BlockWaitAck, ClOrdId)
	logger("write_binary_message_to_redis", "#debug (write_binary_message_to_redis): inserting to redis with Do - HMSET ...")

	_, err := conn.Do("HMSET", msgIndex, "instrumentId", InstrumentId, "symbol", Symbol, "userId", UserId, "side", Side, "ordType", OrdType, "price", Price, "price_scale", Price_scale, "quantity", Quantity, "quantity_scale", Quantity_scale, "nonce", Nonce, "blockWaitAck", BlockWaitAck, "clOrdId", ClOrdId)

	if err != nil {

		logger("write_binary_message_to_redis", "#debug #debug (write_binary_message_to_redis): error writing message to redis: "+err.Error())
		//record as a failure metric
		recordFailureMetrics()
		errCount++

	} else {

		logger("write_binary_message_to_redis", "#debug (write_binary_message_to_redis): wrote message to redis. count: "+strconv.Itoa(msgCount))
		//record as a success metric

		recordSuccessMetrics()
		msgCount++
	}

	return msgCount, errCount
}

func main() {

	//set up the metrics and management endpoint
	//Prometheus metrics UI
	fmt.Println("(debug) setting up metrics endpoint.")
	http.Handle("/metrics", promhttp.Handler())

	//Management UI for Load Data Management
	//Administrative Web Interface
	fmt.Println("(debug) setting up admin portal.")
	admin := newAdminPortal()

	fmt.Println("(debug) setting up admin portal handler.")
	http.HandleFunc("/dumper-admin", admin.handler)

	http.HandleFunc("/dumperselected", admin.selectionHandler)

	//read the load status data (statusHandler)
	fmt.Println("(debug) setting up service status handler.")
	http.HandleFunc("/dumper-status", admin.statusHandler)
	//initiate data download
	fmt.Println("(debug) setting up initialisation handler.")
	http.HandleFunc("/dumper-start", admin.initialiseHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	fmt.Println("(debug) listening on http port ...")
	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start http service endpoint: ", err)
	}

}
