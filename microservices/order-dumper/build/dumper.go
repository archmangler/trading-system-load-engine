package main

import (
	"bufio"
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
var sourceTopic string = os.Getenv("KAFKA_SOURCE_TOPIC")            //main  topic for inbound/historical  orders (api0001)
var kafkaBroker string = os.Getenv("KAFKA_BROKER_SERVICE_ENDPOINT") //kafka broker from order environment
var orderDumpDBIndex = os.Getenv("SEQUENCE_REPLAY_DB")              //ORDER_DUMP_DB_INDEX , index of  the REDIS  db to dump orders in

//data processing from kafka input file
var debug int = 0 //debug switch: 1|0
var symbolLookupDataFile string = "./securities.csv"
var sourceDatafilePath string = "./in-msg.txt"

//Redis data storage details
var redisAuthPass string = os.Getenv("REDIS_PASS")
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER")         // port for metrics service to listen on
var loadStatus string = "pending"                                          //pending|working|done

var kafkaSourceBrokerAddress string = os.Getenv("KAFKA_SOURCE_BROKER_ADDRESS") //address of the kafka broker for dumping historical orders sent to the trade matching system
var orderStreamStartTime string = os.Getenv("ORDER_STREAM_START_TIME")         //e.g ORDER_STREAM_START_TIME = "2022-03-26T00:53:21.000Z"
var orderStreamEndTime string = os.Getenv("ORDER_STREAM_END_TIME")             //e.g ORDER_STREAM_START_TIME = "2022-03-26T01:02:24.000Z"

//This struct extracts whatever is available from the historical order data pulled from kafka
type NewOrderSingleRequest struct {
	Account       int    `json:"account"`
	SecurityId    int    `json:"securityId"`
	Side          string `json:"side"`
	OrdType       string `json:"ordType"`
	Price         int    `json:"price"`
	PriceScale    int    `json:"priceScale"`
	Quantity      int    `json:"qty"`
	QuantityScale int    `json:"qtyScale"`
	ClOrdId       string `json:"clOrdId"`
}

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

func getOrderStream(sourceTopic string, orderStreamStartTime string, orderStreamEndTime string, kafkaBroker string) []string {

	//Dump orders from kafka using kafka tool into a local text file on the pod filesystem (with all the performance hit that implies ...)
	//kafka-dump-tool-0.0.1-SNAPSHOT/bin/run.sh  --kafka-server=kafka1.dexp-qa.internal --kafka-port=4455 -q INPUT --topic=me0001 --output-file=in-msg.txt --start-time=2022-03-26T00:53:21.000Z --end-time=2022-03-26T01:02:24.000Z

	s := make([]string, 0)

	fmt.Println("(getOrderStream) getting order stream from kafka broker ...", kafkaBroker)

	arg1 := "kafka-dump-tool-0.0.1-SNAPSHOT/bin/run.sh"
	arg2 := "--kafka-server=" + kafkaSourceBrokerAddress
	arg3 := "--kafka-port=4455"
	arg4 := "-q INPUT"
	arg5 := "--topic=" + sourceTopic
	arg6 := "--output-file=in-msg.txt"
	arg7 := "--start-time=" + orderStreamStartTime //e.g ORDER_STREAM_START_TIME = "2022-03-26T00:53:21.000Z"
	arg8 := "--end-time=" + orderStreamEndTime     //ORDER_STREAM_START_TIME = "2022-03-26T01:02:24.000Z"

	//debugging message
	fmt.Println("(getOrderStream) ", arg1, " ", arg2, " ", arg3, " ", arg4, " ", arg5, " ", arg6, " ", arg7, " ", arg8)

	out, err := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8).Output()
	temp := strings.Split(string(out), "\n")

	if err != nil {

		fmt.Println("kafka dumper error! ", err)
		fmt.Println("kafka dumper output: ", temp)

	} else {

		fmt.Println("Done dumping orders. Data length: ", len(out))

		loadStatus = "working"

	}
	//fill this with results
	return s

}

func readLines(path string) ([]string, error) {

	// readLines reads a whole file into memory
	// and returns a slice of its lines.

	fmt.Println("(readLines) opening source dump file", path)

	file, err := os.Open(path)

	if err != nil {
		fmt.Println("(readLines) ERROR! opening source dump file", err)

		return nil, err
	}

	defer file.Close()

	var lines []string

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	fmt.Println("(readLines) read lines from order dump file: ", len(lines))

	return lines, scanner.Err()
}

func parseOrderData(content string) map[string]string {
	//parse out relevant fields to a struct

	dMap := make(map[string]string)

	data := NewOrderSingleRequest{}

	json.Unmarshal([]byte(content), &data)

	//these are the only fields we can obtain from kafka right now
	//the additional fields required to assemble the original order
	//will need to be "padded" in later.
	dMap["securityId"] = strconv.Itoa(data.SecurityId) //for lookup of "symbol"
	dMap["userId"] = strconv.Itoa(data.Account)
	dMap["side"] = data.Side
	dMap["ordType"] = data.OrdType
	dMap["price"] = strconv.Itoa(data.Price)
	dMap["price_scale"] = strconv.Itoa(data.PriceScale)
	dMap["quantity"] = strconv.Itoa(data.Quantity)
	dMap["quantity_scale"] = strconv.Itoa(data.QuantityScale)
	dMap["clOrdId"] = string(data.ClOrdId)

	dataMap := fillInTheBlanks(dMap) //make a best guess at the missing values (for now, until better information is available. See: https://eqonex.atlassian.net/browse/EEP-21230)

	return dataMap

}

func lookupSymbols() (s map[string]string) {

	//read a current .csv mapping securityId to symbol name (should be a better way to do this but not sure right now)
	//This means the source .csv file needs to be updated periodically by hand to ensure the securityId can always be
	//translated!
	//Sample:
	//securityId,symbol
	//1,USDC
	//2,ETH
	//3,BTC
	//4,USDT
	//11,BCH
	//25,BTC/USDC[F]

	s = make(map[string]string)

	symbols, err := readLines(symbolLookupDataFile) //symbolLookupDataFile = "securities.csv" in the container /app/ root ...

	if err != nil {
		fmt.Println("(lookupSymbols) there was an error: ", err)
	}

	//loop through line by line
	for line := range symbols {

		if strings.Contains(symbols[line], "NewOrderSingleRequest") {

			//do nothing

		} else {

			data := strings.Split(symbols[line], `,`)
			securityId := data[0]
			symbol := data[1]

			s[securityId] = symbol

			if debug == 1 {
				fmt.Println("(lookupSymbols) ", "securityId = ", securityId, " symbol = ", symbol)
			}
		}
	}

	return s

}

func fillInTheBlanks(d map[string]string) (dataMap map[string]string) {
	//make a best guess at missing values
	//symbolLookupTable := make(map[string]string)

	//lookup the  current symbol/securityId table data
	symbolLookupTable := lookupSymbols()

	/* Data is expected in this form when order is placed at the API:
	{
		1) "instrumentId": 128,                     ... ?
		2) "symbol": "BTC/USDC[Infi]“,       ... ?
		3) "userId": 25098,                          ... "submitterId" ? "account" ?
		4) "side": 1,                                      ... side
		5) "ordType": 2,                              ... "ordType"
		6) "price": 5066816,                        ... price
		7) "price_scale": 2,                          ... priceScale
		8) "quantity": 205800,                     ... qty ?
		9) "quantity_scale": 6,                     ... qtyScale ?
		10)"nonce": 1645427992209,         ... n/a
		11) "blockWaitAck": 0,                      ... n/a
		12) "clOrdId": ""                           ... "clOrdId"
	}
	*/

	securityId := d["securityId"]

	//symbol := symbolLookupTable[securityId]
	symbol := symbolLookupTable[securityId]

	//These values need to be added to the data coming out of kafka
	d["instrumentId"] = "128" //this is an almost certainly wrong default!

	d["symbol"] = symbol //lookup this value from a table

	if debug == 1 {
		fmt.Println("(fillInTheBlanks) symbol => ", d["symbol"])
	}

	d["nonce"] = "0"        //not important at this stage
	d["blockWaitAck"] = "1" //not important at this stage

	return d
}

func processHistoricalOrderStream() {

	//We expect each line in the dump file will have the following format:
	//2022-05-31T01:57:45.652Z|400486014|NewOrderSingleRequest|{"headerTransactionId":0,"headerTransactionEnd":false,"securityId":52,"submitterId":37676,"account":37676,"price":38000,"priceScale":0,"qty":1,"qtyScale":0,"side":"SELL","orderId":0,"orderPriority":0,"secondaryOrderId":0,"clOrdId":"1653962265652380729","apiOrderID":"1653962265652380729-139c1a67-bd7b-4bcd-ab0c-4bb0782e0e6c","ordType":"LIMIT","type":0,"priceInt":0,"quantityLong":0,"quantityOrigLong":0,"quantityOrigScale":0,"timeInForce":"GOOD_TILL_CANCEL","expireTime":0,"stopPx":0,"stopPxScale":0,"stopPxInt":0,"toClose":false,"price2":0,"price2Scale":0,"price2Int":0,"marginCheckReferencePrice":0,"fillCumNotional":0,"origOrderId":0,"origClOrdId":null,"targetStrategy":0,"feeEstimatedQuantity":0,"feeAccumulatedQuantity":0,"availableEstimatedQuantity":0,"availableAccumulatedQuantity":0,"minMaxPrice":0,"returnedStack":null,"apiTimestamp":1653962265575,"pendingReplace":false,"liquidation":false,"lastLook":false,"hidden":false}

	oCnt := 0   //keep track of order entries
	errCnt := 0 //keep track of order entry errors

	theData, err := readLines(sourceDatafilePath)

	if err != nil {
		fmt.Println("(processHistoricalOrderStream) help! there was an error: ", err)
	}

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
		fmt.Println("(processHistoricalOrderStream) redis auth response: ", response)
	}

	//Use defer to ensure the connection is always
	//properly closed before exiting the main() function.
	defer conn.Close()

	//loop through line by line
	for line := range theData {
		//filter constructed here:
		if strings.Contains(theData[line], "NewOrderSingleRequest") && !strings.Contains(theData[line], "TradeState") {
			dataString := strings.Split(theData[line], `|`)

			//To verify the field positioning:
			if debug == 1 {
				for item := range dataString {
					fmt.Println("(processHistoricalOrderStream) split string part ", item, " -> ", dataString[item])
				}
			}

			//We expect the message will be broken down as follows:
			//
			//0  ->  2022-05-31T07:22:25.900Z
			//1  ->  400486482
			//2  ->  NewOrderSingleRequest
			//3  ->  {"headerTransactionId":0,"headerTransactionEnd":false,"securityId":25,"submitterId":30610,"account":30610,"price":0,"priceScale":0,"qty":2443700,"qtyScale":6,"side":"SELL","orderId":0,"orderPriority":0,"secondaryOrderId":0,"clOrdId":"qa_auto__1653981744952_eeYqLS","apiOrderID":"qa_auto__1653981744952_eeYqLS-a75b9a17-4fcc-414e-b964-60bdd495ace3","ordType":"MARKET","type":0,"priceInt":0,"quantityLong":0,"quantityOrigLong":0,"quantityOrigScale":0,"timeInForce":"IMMEDIATE_OR_CANCEL","expireTime":0,"stopPx":0,"stopPxScale":0,"stopPxInt":0,"toClose":false,"price2":0,"price2Scale":0,"price2Int":0,"marginCheckReferencePrice":0,"fillCumNotional":0,"origOrderId":0,"origClOrdId":null,"targetStrategy":0,"feeEstimatedQuantity":0,"feeAccumulatedQuantity":0,"availableEstimatedQuantity":0,"availableAccumulatedQuantity":0,"minMaxPrice":0,"returnedStack":null,"apiTimestamp":1653981745820,"pendingReplace":false,"liquidation":false,"lastLook":false,"hidden":false}

			//The 4th element (3) is the Order data content
			content := dataString[3]

			if debug == 1 {
				fmt.Println(content)
			}

			//TODO: Write this function
			dMap := parseOrderData(content)

			fmt.Println("(processHistoricalOrderStream) ", dMap)

			//process into to redis ...
			//write_binary_message_to_redis(msgCount int, errCount int, msgIndex int, d map[string]string, conn redis.Conn)
			oCnt, errCnt = write_binary_message_to_redis(oCnt, errCnt, line, dMap, conn)
			fmt.Println("(processHistoricalOrderStream) ", " orders inserted: ", oCnt, " order insertion errors: ", errCnt)

		}
	}

	//just in case ...
	loadStatus = "done"
}

func loadHistoricalData(sourceTopic string, oStreamStartTime string, oStreamEndTime string, w http.ResponseWriter, r *http.Request) (string, error) {

	status := "ok"
	loadStatus = "started"
	var err error

	//debug help
	fmt.Println("(loadHistoricalData) order stream start time =", oStreamStartTime, " order stream end time = ", oStreamEndTime)

	w.Write([]byte("<br><html>Using data in " + sourceTopic + " for new load test: " + "</html>"))
	logger("(loadHistoricalData)", "Using data in "+sourceTopic+" for new load test")

	//get available files from source topic
	getOrderStream(sourceTopic, oStreamStartTime, oStreamEndTime, kafkaBroker) //stream messages out from Kafka broker endpoint

	//Default: process historical order data from kafka
	processHistoricalOrderStream()

	loadStatus = "done"

	//report done
	return status, err

}

func (a adminPortal) selectionHandler(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	//Can debug as follows:

	for k, v := range r.Form {
		fmt.Println("(selectionHandler) received url parameter key:", k)
		fmt.Println("(selectionHandler) received url parameter val:", strings.Join(v, ""))
	}

	selection := make(map[string]string)
	params := r.URL.RawQuery

	parts := strings.Split(params, "=")
	selection[parts[0]] = parts[1]

	partsString := strings.Join(parts, " ")
	fmt.Println("(selectionHandler) parameter list: ", partsString)

	//mangling out the parameters
	parameterParts := strings.Split(partsString, "?")
	startParams := parameterParts[0]

	fmt.Println("(selectionHandler) parameterParts[0]: ", startParams)
	stopParams := parameterParts[1]

	fmt.Println("(selectionHandler) parameterParts[1]: ", stopParams)
	start_of_sequence := strings.Split(startParams, " ")[1]
	end_of_sequence := strings.Split(stopParams, " ")[1]

	fmt.Println("(selectionHandler) start, stop params: ", start_of_sequence, end_of_sequence)

	if selection[parts[0]] == "backupProcessedData" {
		fmt.Println("(selectionHandler) backing up used data")
		status, err := backupProcessedData(w, r)

		if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + strconv.Itoa(status) + "</html>"))
		}
	}

	if selection[parts[0]] == "LoadHistoricalData" {

		fmt.Println("(selectionHandler) loading historical data")

		status, err := loadHistoricalData(sourceTopic, start_of_sequence, end_of_sequence, w, r)

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

	r.ParseForm()

	//Can debug as follows:

	for k, v := range r.Form {
		fmt.Println("(initialiseHandler) received url parameter key:", k)
		fmt.Println("(initialiseHandler) received url parameter val:", strings.Join(v, ""))
	}

	selection := make(map[string]string)
	params := r.URL.RawQuery

	parts := strings.Split(params, "=")
	selection[parts[0]] = parts[1]

	partsString := strings.Join(parts, " ")
	fmt.Println("(initialiseHandler) parameter list: ", partsString)

	//mangling out the parameters
	parameterParts := strings.Split(partsString, "?")

	stopParams := parameterParts[0]
	fmt.Println("(initialiseHandler) parameterParts[0]: ", stopParams)

	startParams := parameterParts[1]
	fmt.Println("(initialiseHandler) parameterParts[1]: ", startParams)

	oStreamStartTime := strings.Split(startParams, " ")[1]
	oStreamEndTime := strings.Split(stopParams, " ")[1]

	fmt.Println("(initialiseHandler) start, stop params: ", oStreamStartTime, oStreamEndTime)

	//don't block on this (the user can poll the status url for updates ...)
	go func() {

		status, err := loadHistoricalData(sourceTopic, oStreamStartTime, oStreamEndTime, w, r)

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
	fmt.Println("(write_binary_message_to_redis) inserting : ", InstrumentId, Symbol, UserId, Side, OrdType, Price, Price_scale, Quantity, Quantity_scale, Nonce, BlockWaitAck, ClOrdId)
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
	fmt.Println("(main) setting up metrics endpoint.")
	http.Handle("/metrics", promhttp.Handler())

	//Management UI for Load Data Management
	//Administrative Web Interface
	fmt.Println("(main) setting up admin portal.")
	admin := newAdminPortal()

	fmt.Println("(main) setting up admin portal handler.")
	http.HandleFunc("/dumper-admin", admin.handler)

	http.HandleFunc("/dumperselected", admin.selectionHandler)

	//read the load status data (statusHandler)
	fmt.Println("(main) setting up service status handler.")
	http.HandleFunc("/dumper-status", admin.statusHandler)
	//initiate data download
	fmt.Println("(main) setting up initialisation handler.")
	http.HandleFunc("/dumper-start", admin.initialiseHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	fmt.Println("(main) listening on http port ...")
	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start http service endpoint: ", err)
	}

}
