package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//get running parameters from container environment
var namespace = "ragnarok"                                                       //os.Getenv("POD_NAMESPACE")
var grafana_dashboard_url = os.Getenv("GRAFANA_DASHBOARD_URL")                   // e.g http://192.168.1.4:32000/d/AtqYwRA7k/transaction-matching-system-load-metrics?orgId=1&refresh=10s
var numJobs, _ = strconv.Atoi(os.Getenv("NUM_JOBS"))                             //20
var numWorkers, _ = strconv.Atoi(os.Getenv("NUM_WORKERS"))                       //20
var ingestionServiceAddress string = os.Getenv("INGESTOR_SERVICE_ADDRESS")       //ingestor-service.ragnarok.svc.cluster.local
var replayServiceAddress string = os.Getenv("REPLAY_SERVICE_ADDRESS")            //
var orderDumperServiceAddress string = os.Getenv("ORDER_DUMPER_SERVICE_ADDRESS") //for the service that dumps the last week's order from kafkap topic api0001
var dumpTimeInterval, _ = strconv.Atoi(os.Getenv("ORDER_DUMP_TIME_INTERVAL"))    //hours into the past to dump orders in kafka from

//pulsar connection details
var brokerServiceAddress = os.Getenv("PULSAR_BROKER_SERVICE_ADDRESS") // e.g "pulsar://pulsar-mini-broker.pulsar.svc.cluster.local:6650"
var subscriptionName = os.Getenv("PULSAR_CONSUMER_SUBSCRIPTION_NAME") //e.g sub003
var primaryTopic string = os.Getenv("MESSAGE_TOPIC")                  // "messages"

var source_directory string = os.Getenv("DATA_SOURCE_DIRECTORY") + "/"    // "/datastore"
var processed_directory string = os.Getenv("DATA_OUT_DIRECTORY") + "/"    //"/processed"
var backup_directory string = os.Getenv("BACKUP_DIRECTORY") + "/"         // "/backups"
var logFile string = os.Getenv("LOCAL_LOGFILE_PATH") + "/" + "loader.log" // "/applogs"

var topic1 string = os.Getenv("DEADLETTER_TOPIC")                  // "deadLetter" - for failed message file generation
var topic2 string = os.Getenv("METRICS_TOPIC")                     // "metrics" - for metrics that should be streamed via a topic/queue
var hostname string = os.Getenv("HOSTNAME")                        // "the pod hostname (in k8s) which ran this instance of go"
var start_sequence = 1                                             // start of message range count
var end_sequence = 10000                                           // end of message range count
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER") // port for metrics service to listen on
var taskCount int = 0
var scaleMax, _ = strconv.Atoi(os.Getenv("NUM_JOBS"))
var workerType string = "producer"

//function call count tracking counter
var streamAllCount int = 0

//Redis data storage details
var dbIndex, err = strconv.Atoi(os.Getenv("REDIS_ALLOCATOR_NS_INDEX"))       // Separate namespace for management data. integer index > 0  e.g 2
var sequenceReplayDBindex, _ = strconv.Atoi(os.Getenv("SEQUENCE_REPLAY_DB")) //6
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS")   //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var redisReadConnectionAddress string = os.Getenv("REDIS_REPLICA_ADDRESS")   //address:port combination e.g  "my-release-redis-replicas.default.svc.cluster.local:6379"
var redisAuthPass string = os.Getenv("REDIS_PASS")

//sometimes we operate on global variables ...
var mutex = &sync.Mutex{}

//Data loading variables
var taskMap = make(map[string][]string)
var purgeMap = make(map[string]string) //map of files to 'purge' after processing

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

type OrderAlt struct {
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
	inputFilesGenerated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_files_generated_total",
		Help: "The total number of message files generated for input",
	})

	inputFileWriteErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_files_errors_total",
		Help: "The total number of input message files generation errors",
	})

	assignedOrders = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_allocated_workers_total",
		Help: "The total number of loaded orders allocated to worker processes",
	})

	goJobs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_producer_concurrent_jobs",
		Help: "The total number of concurrent jobs per instance",
	})

	goWorkers = promauto.NewCounter(prometheus.CounterOpts{
		Name: "load_producer_concurrent_workers",
		Help: "The total number of concurrent workers per instance",
	})
)

func recordSuccessMetrics() {
	go func() {
		inputFilesGenerated.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordFailureMetrics() {
	go func() {
		inputFileWriteErrors.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordConcurrentJobs() {
	go func() {
		goJobs.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordConcurrentWorkers() {
	go func() {
		goWorkers.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func recordAssignedOrders() {
	go func() {
		assignedOrders.Inc()
		time.Sleep(2 * time.Second)
	}()
}

func newAdminPortal() *adminPortal {

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

//assign task ranges to workers
func idAllocator(workers []string, taskMap map[int][]string, numWorkers int) (m map[string][]string) {

	fmt.Println("(idAllocator) begin ...")

	tMap := make(map[string][]string)

	element := 0

	for i := range workers {

		taskID := workers[i]

		tMap[taskID] = taskMap[element]

		//debug
		fmt.Println("(idAllocator) assigned ", taskID, " -> ", taskMap[element])

		element++

	}

	fmt.Println("(idAllocator) done ...")

	return tMap
}

func generate_input_sources(startSequence int, endSequence int) (inputs []string) {

	//Generate filenames and ensure the list is randomised
	var inputQueue []string
	logger("(generate_input_sources)", "generating new file names")

	for f := startSequence; f <= endSequence; f++ {
		//IF USING filestorage:		inputQueue = append(inputQueue, inputDir+strconv.Itoa(f))
		inputQueue = append(inputQueue, strconv.Itoa(f))
	}

	//To ensure all worker pods, in a kubernetes scenario, don't operate on the same batch of files at any given time:
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(inputQueue), func(i, j int) { inputQueue[i], inputQueue[j] = inputQueue[j], inputQueue[i] })

	logger("(generate_input_sources)", "	: "+strings.Join(inputQueue, ","))

	return inputQueue
}

func update_management_data_redis(dataIndex string, input_data []string, conn redis.Conn, workCount int) (count int) {

	fmt.Println("(update_management_data_redis) inserting data length: ", len(input_data))

	//switch to correct DB
	response, err := conn.Do("SELECT", dbIndex)
	if err != nil {
		fmt.Println("(update_work_allocation_table) can't connect to redis namespace: ", dbIndex)
		panic(err)
	} else {
		fmt.Println("(update_work_allocation_table) redis select namespace response: ", response)
	}

	//build the message body inputs for json
	for item := range input_data {

		data := input_data[item]

		fmt.Println("(update_management_data_redis) inserting: ", data, " for ", dataIndex)

		_, err := conn.Do("LPUSH", dataIndex, data)

		if err != nil {
			fmt.Println("(update_management_data_redis) failed - LPUSH put data to redis: ", data, err.Error())
		} else {
			recordAssignedOrders()
			fmt.Println("(update_management_data_redis) ok - LPUSH put data to redis: ", data)
		}

	}

	workCount++

	fmt.Println("(update_management_data_redis) updating work count: ", workCount)
	return workCount
}

//Creatively Synthesise random trades from nothing
func fabricate_order(ts int64, msgIndex string, conn redis.Conn) (err error) {

	//Sample: {"instrumentId":128,"symbol":"BTC/USDC[Infi]","userId":xxxx,"side":2,"ordType":2,"price":5173054,"price_scale":2,"quantity":61300,"quantity_scale":6,"nonce":1645495020701,"blockWaitAck":0,"clOrdId":""}
	rand.Seed(time.Now().UnixNano())

	rangeLower := 1000000
	rangeUpper := 5200000
	randomNum := rangeLower + rand.Intn(rangeUpper-rangeLower+1)

	InstrumentId := 128
	Symbol := "BTC/USDC[Infi]"
	UserId := 25777 //get's changed further upstream anyway
	Side := 2
	OrdType := 2
	Price := randomNum //randomize!
	Price_scale := 2

	rangeLower = 10000
	rangeUpper = 65000
	randomNum = rangeLower + rand.Intn(rangeUpper-rangeLower+1)

	Quantity := randomNum // randomize
	Quantity_scale := 6

	rangeLower = 1000000000001
	rangeUpper = 1845495020701
	randomNum = rangeLower + rand.Intn(rangeUpper-rangeLower+1)

	Nonce := randomNum //randomize
	BlockWaitAck := 1
	ClOrdId := hostname

	_, err = conn.Do("HMSET", msgIndex, "instrumentId", InstrumentId, "symbol", Symbol, "userId", UserId, "side", Side, "ordType", OrdType, "price", Price, "price_scale", Price_scale, "quantity", Quantity, "quantity_scale", Quantity_scale, "nonce", Nonce, "blockWaitAck", BlockWaitAck, "clOrdId", ClOrdId)

	return err
}

func put_to_redis(msgId string, fileCount int, conn redis.Conn) (fc int) {

	// Send our command across the connection. The first parameter to
	// Do() is always the name of the Redis command (in this example
	// HMSET), optionally followed by any necessary arguments (in this
	// example the key, followed by the various hash fields and values).

	now := time.Now()
	msgTimestamp := now.UnixNano() //build the message body inputs for json
	//make up an imaginary trade
	err := fabricate_order(msgTimestamp, msgId, conn)

	if err != nil {

		fmt.Println("failed to put data to redis: ", msgId, err.Error())
		//record as a failure metric
		recordFailureMetrics()

	} else {

		//record as a success metric
		recordSuccessMetrics()
		fileCount++

	}

	return fileCount

}

func create_load_file(input_file string, fIndex int, fileCount int) (fc int) {

	logger(logFile, "trying to create input file: "+input_file)

	//format the request message payload (milliseconds for now)
	now := time.Now()
	msgTimestamp := now.UnixNano()

	msgPayload := `[{ "Name": "newOrder","ID":"` + strconv.Itoa(fIndex) + `","Time":"` + strconv.FormatInt(msgTimestamp, 10) + `","Data":"` + hostname + `","Eventname":"transactionRequest"}]`

	logger(logFile, "prepared message payload: "+msgPayload)

	f, err := os.Create(input_file)

	if err != nil {
		logger(logFile, "error generating request message file: "+err.Error())

		//record as a failure metric
		recordFailureMetrics()
	} else {
		//record as a success metric
		recordSuccessMetrics()
		fileCount++
	}
	_, err = f.WriteString(msgPayload)
	f.Close()

	fmt.Println("[debug] data -> ", msgPayload, " Error: ", err)
	return fileCount
}

func process_input_data(tmpFileList []string) int {

	logger(logFile, "processing files: "+strings.Join(tmpFileList, ","))

	//where the actual task gets done
	//To Store in file:
	//fileCount = create_load_file(input_file, fIndex, fileCount)
	//Alternatively: to store in Redis
	// Establish a connection to the Redis server listening on port
	// 6379 of the local machine. 6379 is the default port, so unless
	// you've already changed the Redis configuration file this should
	// work.

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

	fileCount := 0

	for fIndex := range tmpFileList {

		inputData := tmpFileList[fIndex]

		fileCount = put_to_redis(inputData, fileCount, conn)

	}

	logger(logFile, "done creating input files.")

	return fileCount
}

func MoveFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("Couldn't open source file: %s", err)
	}
	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("Couldn't open dest file: %s", err)
	}

	_, err = io.Copy(outputFile, inputFile)

	inputFile.Close()
	outputFile.Close()

	if err != nil {
		return fmt.Errorf("Writing to output file failed: %s", err)
	}

	// The copy was successful, so now delete the original file
	err = os.Remove(sourcePath)
	if err != nil {
		return fmt.Errorf("Failed removing original file: %s", err)
	}

	return err
}

func backupProcessedData(w http.ResponseWriter, r *http.Request) (int, error) {

	ocount := 0

	logger("(backupProcessedData)", "calling backup service to backup  load queue ...")

	w.Write([]byte("<html><h1>calling backup service to backup  load queue  ...</h1><br> files </html>"))

	//Call backup function via the replay service API
	backupOrderQueue()

	//return count of files backed up
	return ocount, err
}

func get_worker_pool(workerType string, namespace string) (workers []string, count int) {

	count = 0

	arg1 := "kubectl"
	arg2 := "get"
	arg3 := "pods"
	arg4 := "--namespace"
	arg5 := namespace
	arg6 := "--field-selector"
	arg7 := "status.phase=Running"
	arg8 := "--no-headers"
	arg9 := "-o"
	arg10 := "custom-columns=:metadata.name"

	cmd := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8, arg9, arg10)

	logger("get_worker_pool", "Running command: "+arg1+" "+arg2+" "+arg3+" "+arg4+" "+arg5+" "+arg6+" "+arg7+" "+arg8+" "+arg9+" "+arg10)

	time.Sleep(5 * time.Second) //really should have a loop here waiting for returns ...
	out, err := cmd.Output()

	if err != nil {

		logger("get_worker_pool", "cannot get worker list "+err.Error())

	} else {

		logger("get_worker_pool", "got current worker list - ok")

	}

	temp := strings.Split(string(out), "\n")

	for line := range temp {

		w := temp[line]

		if strings.Contains(w, workerType) {
			count++
			workers = append(workers, w)
			logger("get_worker_pool", "increment worker count: "+strconv.Itoa(count))
		}

	}

	return workers, count

}

func update_work_allocation_table(workAllocationMap map[string][]string, workCount int) (wC int) {

	//Connect to redis
	conn, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		panic(err)
	} else {
		fmt.Println("(update_work_allocation_table) redis auth response: ", response)
	}

	//Use defer to ensure the connection is always
	//properly closed before exiting the main() function.
	defer conn.Close()

	//map of assigned work (worker pod -> array of message ids)

	for k, v := range workAllocationMap {

		fmt.Println("(update_work_allocation_table) store work map item: ", k, " -> ", v, " count: ", workCount)

		workCount = update_management_data_redis(k, v, conn, workCount)

	}

	//keep a counter of how many worker entries were written
	logger("main", "(update_work_allocation_table) wrote work map for "+strconv.Itoa(workCount)+" workers")

	response, err = conn.Do("SELECT", 0)

	return workCount
}

func assign_message_workload_workers(workers []string, inputs []string, numWorkers int) (m map[string][]string) {

	tempMap := make(map[int][]string)
	size := len(inputs) / numWorkers

	if size < 1 {
		size = 1
	}

	fmt.Println("(assign_message_workload_workers) begin creating work allocation template, with inputs length: ", len(inputs), " allocation size: ", size, " workers: ", workers)

	var j int
	var lc int = 0

	for i := 0; i < len(inputs); i += size {

		fmt.Println("(assign_message_workload_workers) lc,i,j -> ", lc, i, j)

		j += size

		if j > len(inputs) {

			j = len(inputs)

		}

		//just populate the map for now
		//later assign worker-job pairs
		tempMap[lc] = inputs[i:j]

		lc++

	}

	fmt.Println("(assign_message_workload_workers) calling work id allocator.")

	tMap := idAllocator(workers, tempMap, numWorkers)

	fmt.Println("(assign_message_workload_workers) created work allocation template: ", tMap)

	return tMap
}

func deleteFromRedis(inputId string, conn redis.Conn) (err error) {

	//delete stale allocation data from the previous work allocation table

	err = nil

	//build the message body inputs for json
	fmt.Println("getting data for key: ", inputId)

	//switch to metadata db
	conn.Do("SELECT", dbIndex)

	msgPayload, err := conn.Do("DEL", inputId)

	if err != nil {

		fmt.Println("OOPS, got this: ", err, " skipping ", inputId)

		return err

	} else {

		fmt.Println("OK - delete got this: ", msgPayload)

	}

	return nil
}

func delete_stale_allocation_data(workerIdList []string) {

	fmt.Println("(delete_stale_allocation_data) deleting old allocation directory ...")

	//Open Redis connection here again ...
	conn, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		fmt.Println("(delete_stale_allocation_data) redis auth response: ", response)
		panic(err)
	} else {
		fmt.Println("(delete_stale_allocation_data) redis auth response: ", response)
	}

	defer conn.Close()

	for wId := range workerIdList {

		workerId := workerIdList[wId]
		err := deleteFromRedis(workerId, conn)

		if err != nil {

			logMessage := "Failed to read data for " + workerId + " error code: " + err.Error()
			logger("(delete_stale_allocation_data)", logMessage)

		} else {

			logMessage := "OK: read payload data for " + workerId
			logger("(delete_stale_allocation_data)", logMessage)

		}

	}

	fmt.Println("(delete_stale_allocation_data) DONE ...")
}

func loadSyntheticData(w http.ResponseWriter, r *http.Request, start_sequence int, end_sequence int) (string, error) {

	logger(logFile, "(loadSyntheticData) Creating synthetic data for workload ...")

	status := "ok"
	var err error

	w.Write([]byte("<html><h1>Generating and Loading Synthetic Data</h1></html>"))

	//Generate input file list and distribute among workers
	inputQueue := generate_input_sources(start_sequence, end_sequence) //Generate the total list of inputs
	metadata := strings.Join(inputQueue, ",")
	logger(logFile, "(loadSyntheticData) generated new workload metadata: "+metadata)

	//Allocate workers to the input data
	workCount := 0
	namespace := "ragnarok"

	//get the currently deployed worker pods in the producer pool
	workers, cnt := get_worker_pool(workerType, namespace)

	//delete the current work allocation table as it is now stale data
	delete_stale_allocation_data(workers)

	logger(logFile, "(loadSyntheticData) done deleting stale allocation data. Preparing to create work allocation map ...")

	//Assign message workload to worker pods
	workAllocationMap := assign_message_workload_workers(workers, inputQueue, cnt)

	//Update the work allocation in a REDIS database
	workCount = update_work_allocation_table(workAllocationMap, workCount)

	//update this global

	scaleMax = workCount

	w.Write([]byte("<br><html>updated worker allocation table for concurrent message processing.</html>"))

	//Generate the actual message  data.
	fcnt := process_input_data(inputQueue)

	w.Write([]byte("<br><html>Generated new load test data: " + strconv.Itoa(fcnt) + " files." + "</html>"))

	return status, err
}

func loadHistoricalData(w http.ResponseWriter, r *http.Request) (string, error) {

	status := "pending"
	var err error

	w.Write([]byte("<br><html>Using historical order data for new load test ...</html>"))
	logger(logFile, "Loading past captured orders ...")

	//[x] 0. Read ingestor status: GET /ingest-status
	resp, err := http.Get(ingestionServiceAddress + "/ingest-status")

	if err != nil {
		fmt.Println("(loadHistoricalData) error reading ingest status endpoint: ", err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(loadHistoricalData) error reading ingest status response body: ", err)
	}

	status = string(body)
	fmt.Println("(loadHistoricalData) Getting ingestor service status: ", status)

	w.Write([]byte("<br><html>Getting ingestor service status: " + status + "</html>"))

	//[x] 1. Trigger the loader service via the API (kubectl delete <pod-name>)
	// GET /load-start
	resp, err = http.Get(ingestionServiceAddress + "/ingest-start")

	if err != nil {
		log.Fatalln(err)
	}

	body, err = ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(loadHistoricalData) error reading ingest status endpoint: ", err)
	}

	status = string(body)
	fmt.Println("triggered ingestion start: ", status)

	//	[x] 2. Wait until it's status "ok" (until /ingest-status/ is not `pending`)
	//	  [x] 2.1. Poll the status endpoint ("API": curl /load-status/) pending|started|completed

	for {
		resp, err = http.Get(ingestionServiceAddress + "/ingest-status")

		if err != nil {
			fmt.Println("(loadHistoricalData) error getting ingest status endpoint: ", err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(loadHistoricalData) error reading ingest status response body: ", err)
			break
		}

		status = string(body)

		fmt.Println("(loadHistoricalData) Getting ingestor service status: ", status)

		if status == "done" {
			fmt.Println("(loadHistoricalData) finished ingesting input data: ", status)
			w.Write([]byte("<br><html>finished ingesting input data: " + status + "</html>"))
			break
		} else {
			w.Write([]byte("<br><html>ingestor status: " + status + "</html>"))
		}

		time.Sleep(5 * time.Second)
	}

	/*
	 [p] 3. Trigger the load-distribition process (distribute load among available workers)
	  [p] 3.1 call function to reload producers
	*/
	fmt.Println("(loadHistoricalData) Getting load data from local storage ...")
	w.Write([]byte("<br><html>Getting load data from local storage</html>"))

	//load data from DB index 3 to index 0 in REDIS
	//[x] 3.2 load the data out  of DB 3
	//[x] 3.3 load the data into DB 0
	bulkOrderLoad(3)

	/*
	  [x] 4. Execute the load test ("ready to run load test") or give user a trigger load test button (???)
	*/

	fmt.Println("(loadHistoricalData) Ready to start load test.")
	w.Write([]byte("<br><html>Data is loaded for load test. </html>"))

	//report done
	return status, err

}

func markUserCredentialsUnused() (status string) {
	//simply call the decicated golang script that implements the function
	//within the container filesystem, this should be: "/app/loadcreds"

	arg1 := "/app/loadcreds"

	status = "unknown"

	cmd := exec.Command(arg1)

	logger("(markUserCredentialsUnused)", "Running command: "+arg1)

	out, err := cmd.Output()

	if err != nil {
		logger(logFile, "(markUserCredentialsUnused) error. "+err.Error())
		return "failed"

	} else {
		logger("(markUserCredentialsUnused)", "refreshed synthetic user credentials - ok")
		status = "ok"
	}

	logger("(markUserCredentialsUnused)", "refresh command result: "+string(out))

	return status

}

func (a adminPortal) selectionHandler(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	selection := make(map[string]string)
	params := r.URL.RawQuery

	parts := strings.Split(params, "=")
	selection[parts[0]] = parts[1]

	if selection[parts[0]] == "backupProcessedData" {
		fmt.Println("running load queue backups")
		status, err := backupProcessedData(w, r)

		if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + strconv.Itoa(status) + "</html>"))
		}
	}

	if selection[parts[0]] == "LoadSyntheticData" {

		//User can select the start sequence file name and end
		start_of_sequence := strings.Join(r.Form["start"], " ")
		end_of_sequence := strings.Join(r.Form["stop"], " ")

		w.Write([]byte("<html><h1>Creating input load data from topic sequence ... </h1></html>"))

		w.Write([]byte("<html style=\"font-family:verdana;\"><h1>Generate new input load data ... </h1></html>"))
		w.Write([]byte("<html style=\"font-family:verdana;\">start of sequence: " + start_of_sequence + "<br></html>"))
		w.Write([]byte("<html style=\"font-family:verdana;\">end of sequence: " + end_of_sequence + "<br></html>"))

		so_seq, _ := strconv.Atoi(start_of_sequence)
		eo_seq, _ := strconv.Atoi(end_of_sequence)

		status, err := loadSyntheticData(w, r, so_seq, eo_seq)

		if err != nil {
			w.Write([]byte("<html> Woops! ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> selection result: " + status + "</html>"))
		}
	}

	if selection[parts[0]] == "LoadRequestSequence" {

		start_of_sequence := strings.Join(r.Form["start"], " ")
		end_of_sequence := strings.Join(r.Form["stop"], " ")

		w.Write([]byte("<html><h1>Creating input load data from topic sequence ... </h1></html>"))

		w.Write([]byte("<html style=\"font-family:verdana;\"><h1 >Creating input load data from topic sequence ... </h1></html>"))
		w.Write([]byte("<html style=\"font-family:verdana;\">start of sequence: " + start_of_sequence + "<br></html>"))
		w.Write([]byte("<html style=\"font-family:verdana;\">end of sequence: " + end_of_sequence + "<br></html>"))

		fmt.Println("(selectionHandler) got parameter strings: ", start_of_sequence, end_of_sequence)

		serial := 0 //stream the orders to the api in original order 1 or in parallel 0

		bootStrapOrderData(sequenceReplayDBindex, start_of_sequence, end_of_sequence, serial)

	}

	if selection[parts[0]] == "LoadHistoricalData" {

		w.Write([]byte("<html><h1>Creating input load data from legacy orders ... </h1></html>"))

		status, err := loadHistoricalData(w, r)

		if err != nil {
			w.Write([]byte("<html> Woops! failed to trigger data ingestion ... " + err.Error() + "</html>"))
		} else {
			w.Write([]byte("<html> Ok! ingestion result: " + status + "</html>"))
		}
	}

	if selection[parts[0]] == "RestartLoadTest" {
		status := "null"

		w.Write([]byte("<html><h1>Resetting and restarting load test </h1></html>"))

		//restart all services that need re-initialisation for a new load test
		_, scaleMax := get_worker_pool(workerType, namespace)
		status = restart_loading_services("producer", scaleMax, namespace, w, r)

		w.Write([]byte("<html> <br>Restarted producers - " + status + "</html>"))

	}

	//no matter what, always call the credential status refresh
	//to make sure each management function call unlocks synthetic
	//user credentials for use
	markUserCredentialsUnused()

	html_content := `
	<body>
	<br>
    <form action="/loader-admin" method="get">
           <input type="submit" name="back" value="back to main page">
	</form>
	</body>
	`
	w.Write([]byte(html_content))
}

func (a adminPortal) handler(w http.ResponseWriter, r *http.Request) {

	//Very Crude web-based option selection menu defined directly in html, no templates, no styles ...
	//<Insert Authentication Code Here>

	html_content := `
	<html><h1 style="font-family:verdana;">Transaction Matching Load Test Workbench</h1><br></html>
	<body>
	<div style="padding:10px;">
	<h3 style="font-family:verdana;">Select Load Testing Operations:</h3>
	  <br>


  <form action="/selected?param=LoadHistoricalData" method="post">
  <input type="submit" name="LoadHistoricalData" value="load from historical data" style="padding:20px;">
  <br>
  </form>

  <form action="/selected?param=RestartLoadTest" method="post">
  <input type="submit" name="RestartLoadTest" value="restart / reset load test" style="padding:20px;">
  <br>
  </form>
 
  <form action="/selected?param=LoadSyntheticData" method="post">
          <input type="submit" name="LoadSyntheticData" value="load from synthetic data" style="padding:20px;">
  <html style="font-family:verdana;">Start of sequence:</html><input type="text" name="start" >
  <html style="font-family:verdana;">End of Sequence:</html><input type="text" name="stop" >
  </form>

  <form action="/selected?param=LoadRequestSequence" method="post"">
  <input type="submit" name="LoadRequestSequence" value="load request sequence" style="padding:20px;" style="font-family:verdana;"> 
  <html style="font-family:verdana;">Start time of sequence (format: 2022-06-04T15:49:43.000Z):</html><input type="text" name="start" >
  <html style="font-family:verdana;">End time of Sequence (format: 2022-06-04T15:49:43.000Z):</html><input type="text" name="stop" >
  </form>

  <form action="/selected?param=backupProcessedData" method="post">
    <input type="submit" name="backupProcessedData" value="backup order queue" style="padding:20px;">
  </form>

  </div>
	<div>
		<a href="` + grafana_dashboard_url + `">Load Testing Metrics Dashboard</a>
	</div>
	</body>
	`
	w.Write([]byte(html_content))
}

func restart_loading_services(service_name string, sMax int, namespace string, w http.ResponseWriter, r *http.Request) string {

	//scale down to 0, then scale up to the current max.
	arg1 := "kubectl"
	arg2 := "scale"
	arg3 := "statefulset"
	arg4 := service_name
	arg5 := "--replicas=0"
	arg6 := "--namespace"
	arg7 := namespace

	status := "unknown"

	cmd := exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)
	logger(logFile, "Running command: "+arg1+" "+arg2+" "+arg3+" "+arg4+" "+arg5+" "+arg6+" "+arg7)

	time.Sleep(5 * time.Second) //really should have a loop here waiting for returns ...

	out, err := cmd.Output()

	if err != nil {
		logger(logFile, "cannot stop component: "+service_name+" error. "+err.Error())
		return "failed"

	} else {
		logger(logFile, "restarted service - ok")
		status = "ok"
	}

	temp := strings.Split(string(out), "\n")
	theOutput := strings.Join(temp, `\n`)
	logger(logFile, "restart command result: "+theOutput)

	//for the user
	w.Write([]byte("<html> <br>scale down service status: " + theOutput + "</html>"))

	arg1 = "kubectl"
	arg2 = "scale"
	arg3 = "statefulset"
	arg4 = service_name
	arg5 = "--replicas=" + strconv.Itoa(sMax)
	arg6 = "--namespace"
	arg7 = namespace

	//scale up
	cmd = exec.Command(arg1, arg2, arg3, arg4, arg5, arg6, arg7)

	logger(logFile, "Running command: "+arg1+" "+arg2+" "+arg3+" "+arg4+" "+arg5+" "+arg6+" "+arg7)

	time.Sleep(5 * time.Second)

	out, err = cmd.Output()

	if err != nil {

		logger(logFile, "cannot get status for component: "+service_name+" error. "+err.Error())
		return "failed"

	} else {

		logger(logFile, "got service status - ok")
		status = "ok"

	}

	temp = strings.Split(string(out), "\n")
	theOutput = strings.Join(temp, `\n`)
	logger(logFile, "restart command result: "+theOutput)

	//for the user
	w.Write([]byte("<html> <br>service status: " + theOutput + "</html>"))

	logger(logFile, "done resetting system components for "+service_name)
	return status
}

//modify to use pulsar
func write_message_from_pulsar(msgCount int, errCount int, temp []string, line int) (int, int) {

	file_name := source_directory + strconv.Itoa(msgCount) + ".json"

	//write each line to an individual file
	f, err := os.Create(file_name)

	if err != nil {
		logger("write_message_from_pulsar", "error generating request message file: "+err.Error())
		//record as a failure metric
		errCount++

	} else {
		logger("write_message_from_pulsar", "wrote historical topic message to file: "+file_name)
		//record as a success metric
		msgCount++
	}

	logger("write_message_from_pulsar", "will write this topic message to file: "+temp[line])
	_, err = f.WriteString(temp[line])
	f.Close()
	return msgCount, errCount
}

func write_binary_message_to_redis(msgCount int, errCount int, msgIndex int64, d map[string]string, conn redis.Conn) (int, int) {

	logger("write_binary_message_to_redis", "#debug (write_binary_message_to_redis)")
	fmt.Println("#debug (write_binary_message_to_redis) data map: ", d)

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
	conn.Do("SELECT", 0)

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
		msgCount++
		recordSuccessMetrics()
	}

	return msgCount, errCount
}

//custom parsing of JSON struct
//Expected format as read from Pulsar topic: [{ "Name":"newOrder","ID":"14","Time":"1644469469070529888","Data":"loader-c7dc569f-8bkql","Eventname":"transactionRequest"}]
//{"instrumentId":128,"symbol":"BTC/USDC[Infi]","userId":25097,"side":2,"ordType":2,"price":5173054,"price_scale":2,"quantity":61300,"quantity_scale":6,"nonce":1645495020701,"blockWaitAck":0,"clOrdId":""}

func parseJSONmessage(theString string) map[string]string {

	dMap := make(map[string]string)

	//theString = strings.Trim(theString, "[")
	//theString = strings.Trim(theString, "]")

	fmt.Println("(parseJSONmessage) trimmed payload string ", theString)

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

	fmt.Println("field extraction: ", dMap)

	return dMap
}

func parseJSONmessageAlt(theString string) map[string]string {

	dMap := make(map[string]string)

	theString = strings.Trim(theString, "[")
	theString = strings.Trim(theString, "]")

	fmt.Println("(parseJSONmessageAlt) trimmed payload string ", theString)

	data := OrderAlt{}
	json.Unmarshal([]byte(theString), &data)

	fmt.Println("(parseJSONmessageAlt) post marshalling payload: ", data)

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

	fmt.Println("(parseJSONmessageAlt) field extraction: ", dMap)

	return dMap
}

//reach out to separate sequence streaming service to load the required sequence into REDIS
func dump_pulsar_messages_to_input(msgStartSeq int, msgStopSeq int) (status string, err error) {

	fmt.Println("(dump_pulsar_messages_to_input) got parameters: ", msgStartSeq, msgStopSeq)

	status, err = loadOrderSequence(msgStartSeq, msgStopSeq)

	return status, err

}

func loadOrderSequence(msgStartSeq int, msgStopSeq int) (status string, err error) {

	status = "pending"

	fmt.Println("(loadOrderSequence) got parameters: ", msgStartSeq, msgStopSeq)
	logger(logFile, "(loadOrderSequence) loading historical order sequence from queue ...")

	//[x] 0. Read replay status: GET /streamer-status
	resp, err := http.Get(replayServiceAddress + "/streamer-status")

	if err != nil {
		fmt.Println("(loadOrderSequence) http read error getting /streamer-status", err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(loadOrderSequence) http read error reading response body", err)
	}

	status = string(body)

	fmt.Println("(loadOrderSequence) Getting replay service status: ", status)

	//[x] 1. Trigger the loader service via the API (kubectl delete <pod-name>)
	// GET /load-start
	fmt.Println("(loadOrderSequence) start=", msgStartSeq, " stop=", msgStopSeq)

	//don't block on this ...
	//go func() {
	resp, err = http.Get(replayServiceAddress + "/streamer-start?start=" + strconv.Itoa(msgStartSeq) + "?stop=" + strconv.Itoa(msgStopSeq))

	if err != nil {
		log.Fatalln(err)
	}

	body, err = ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(loadOrderSequence) http read error reading streamer-start response body", err)
	}

	status = string(body)
	fmt.Println("(loadOrderSequence) triggered replay start: ", status)

	//}()
	//	[x] 2. Wait until it's status "reloaded" (until /streamer-status/ is not `pending`)
	//	  [x] 2.1. Poll the status endpoint ("API": curl /streamer-status/) pending|started|completed

	for {
		resp, err = http.Get(replayServiceAddress + "/streamer-status")

		if err != nil {
			fmt.Println("(loadOrderSequence) error getting /streamer-status endpoint", err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(loadOrderSequence) error reading streamer-status response body", err)
			break
		}

		status = string(body)

		fmt.Println("(loadOrderSequence) Getting replay service status: ", status)

		if status == "reloaded" {
			fmt.Println("(loadOrderSequence) finished replaying input data sequence: ", status)
			break
		}

		time.Sleep(5 * time.Second)
	}

	/*
	 [p] 3. Trigger the load-distribition process (distribute load among available workers)
	  [p] 3.1 call function to reload producers
	*/

	fmt.Println("(loadOrderSequence) Getting replay load data from local storage ...")
	//w.Write([]byte("<br><html>Getting replay load data from local storage</html>"))

	//load data from DB index 3 to index 0 in REDIS
	//[x] 3.2 load the data out  of DB 5
	//[x] 3.3 load the data into DB 0

	//#WARNING# :  need to first truncate DB6 to get rid of stale loads ...
	bulkOrderLoad(sequenceReplayDBindex)

	/*
	  [x] 4. Execute the load test ("ready to run load test") or give user a trigger load test button (???)
	*/

	fmt.Println("(loadOrderSequence) Ready to start load test with replayed data sequence.")
	//w.Write([]byte("<br><html>Data is loaded for replay load test. </html>"))

	//report done
	return status, err

}

//BEGIN: Data loading code
//Can we avoid this? It's slow ...
func purgeProcessedRedis(conn redis.Conn, inputDBIndex int) {
	//purge the originally loaded data once processed into db 0

	purgeCntr := 0

	logger(logFile, "(purgeProcessedRedis) purging processed files ... "+strconv.Itoa(purgeCntr))

	//have to select the data staging DB (data input source)
	result, err := conn.Do("SELECT", inputDBIndex)

	if err != nil {

		fmt.Println("(purgeProcessedRedis) failed to select redis db: ", inputDBIndex, err.Error())

	} else {

		fmt.Println("(purgeProcessedRedis) selecting redis db", inputDBIndex, result)

	}

	for input_id := range purgeMap {

		fmt.Println("(purgeProcessedRedis) purging redis item ", input_id)

		result, err = conn.Do("DEL", input_id)

		if err != nil {
			fmt.Printf("(purgeProcessedRedis) Failed removing original input data from redis: %s\n", err)
		} else {
			fmt.Printf("(purgeProcessedRedis) deleted input %s -> %s\n", input_id, result)
		}

		purgeCntr++
	}

	logger(logFile, "(purgeProcessedRedis) purged processed files: "+strconv.Itoa(purgeCntr))

	//Switch back to default DB
	_, errSelect := conn.Do("SELECT", 0)

	if errSelect != nil {

		fmt.Println("(purgeProcessedRedis) FAILED to switch back to default REDIS DB")

	} else {

		fmt.Println("(purgeProcessedRedis) Switched back to default REDIS DB: ", 0)

	}

}

func readFromRedis(input_id string, conn redis.Conn) (ds string, err error) {

	msgPayload := ""
	err = nil

	//build the message body inputs for json
	//      _, err = conn.Do("HMSET", msgIndex, "instrumentId", InstrumentId, "symbol", Symbol, "userId", UserId, "side", Side, "ordType", OrdType, "price", Price, "price_scale", Price_scale, "quantity", \
	// Quantity, "quantity_scale", Quantity_scale, "nonce", Nonce, "blockWaitAck", BlockWaitAck, "clOrdId", ClOrdId)

	InstrumentId, err := redis.String(conn.Do("HGET", input_id, "instrumentId"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Symbol, err := redis.String(conn.Do("HGET", input_id, "symbol"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	UserId, err := redis.String(conn.Do("HGET", input_id, "userId"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Side, err := redis.String(conn.Do("HGET", input_id, "side"))
	if err != nil {
		fmt.Println("oops, got thi:wqs: ", err, " skipping ", input_id)
		return input_id, err
	}

	OrdType, err := redis.String(conn.Do("HGET", input_id, "ordType"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Price, err := redis.String(conn.Do("HGET", input_id, "price"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Price_scale, err := redis.String(conn.Do("HGET", input_id, "price_scale"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Quantity, err := redis.String(conn.Do("HGET", input_id, "quantity"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Quantity_scale, err := redis.String(conn.Do("HGET", input_id, "quantity_scale"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	Nonce, err := redis.String(conn.Do("HGET", input_id, "nonce"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	BlockWaitAck, err := redis.String(conn.Do("HGET", input_id, "blockWaitAck"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	ClOrdId, err := redis.String(conn.Do("HGET", input_id, "clOrdId"))
	if err != nil {
		fmt.Println("oops, got this: ", err, " skipping ", input_id)
		return input_id, err
	}

	//pack the fields into json structure, this has performance impact
	//but allows us to insert a tracing id into the data (try putting it into the metadata instead)
	//We should marshall this json using a well defined struct but lets
	//take the shortcut for now ...
	msgPayload = `[{"instrumentId":"` + InstrumentId +
		`","symbol":"` + Symbol +
		`","userId":"` + UserId +
		`","side":"` + Side +
		`","ordType":"` + OrdType +
		`","price":"` + Price +
		`","price_scale":"` + Price_scale +
		`","quantity":"` + Quantity +
		`","quantity_scale":"` + Quantity_scale +
		`","nonce":"` + Nonce +
		`","blockWaitAck":"` + BlockWaitAck +
		`","clOrdId":"` + ClOrdId + `"}]`

	fmt.Println("got msg from redis ->", msgPayload, "<-")

	//get all the required data for the input id and return as json string
	return msgPayload, err

}

func process_input_data_redis_concurrent(workerId int, jobId int, inputDBIndex int) {

	//error counter
	errCount := 0
	msgCount := 0

	var tmpFileList []string
	var allData = map[int64]map[string]string{}

	//Allocate workers to the input data
	var workCount int = 0
	var namespace string = "ragnarok"

	//debug
	fmt.Println("(process_input_data_redis_concurrent) task map: ", taskMap)

	//Get the unique key for the set of input tasks for this worker-job combination
	taskID := strconv.Itoa(workerId) // can be keyed with: jobId + "-" + strconv.Itoa(jobNum)
	tmpFileList = taskMap[taskID]

	//Open Redis connection here again ...
	conn, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		log.Fatal(err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		panic(err)
	} else {
		fmt.Println("(process_input_data_redis_concurrent) redis auth response: ", response)
	}

	defer conn.Close()

	//SELECT redis DB 3 to get the raw order data, Select DB 5 for replayed queue messages
	fmt.Println("(process_input_data_redis_concurrent) select redis db: ", inputDBIndex)
	conn.Do("SELECT", inputDBIndex)

	fmt.Println("(process_input_data_redis_concurrent) file list -> ", tmpFileList)

	orderCounter := 0

	for fIndex := range tmpFileList {

		orderCounter++

		input_id := tmpFileList[fIndex]

		payload, err := readFromRedis(input_id, conn) //readFromRedis(input_id string, c conn.Redis) (ds string, err error)

		fmt.Println("(process_input_data_redis_concurrent) payload -> ", payload)

		if err != nil {

			errCount++

			logMessage := "(process_input_data_redis_concurrent) FAILED: " + strconv.Itoa(workerId) + " failed to read data for " + input_id + " error code: " + err.Error()

			logger("(process_input_data_redis_concurrent)", logMessage)

		} else {

			logMessage := "(process_input_data_redis_concurrent) OK: " + strconv.Itoa(workerId) + " read payload data for " + input_id
			logger("(process_input_data_redis_concurrent)", logMessage)

			fmt.Println("(process_input_data_redis_concurrent) Read payload from Redis: ", payload)

			//write the actual payload to redis db 0
			dataMap := parseJSONmessageAlt(payload)
			allData[int64(fIndex)] = dataMap

			//debug printout
			for f := range dataMap {
				fmt.Println("(process_input_data_redis_concurrent): got struct field from map -> ", f, dataMap[f])
			}

			//keep a record of files that should be moved to /processed after the workers stop
			mutex.Lock()
			purgeMap[input_id] = input_id
			mutex.Unlock()

			fmt.Println("(process_input_data_redis_concurrent) completed job: ", jobId)
		}

		fmt.Println("(process_input_data_redis_concurrent) [checkpoint] handling input message index: ", input_id, tmpFileList[fIndex], " order loop count: ", orderCounter)

	}

	//#debug
	fmt.Println("(process_input_data_redis_concurrent): done building purge map")

	//write data to redis db 0 for processing by producers
	fmt.Println("(process_input_data_redis_concurrent) select redis db: ", 0)
	conn.Do("SELECT", 0)

	lCount := 0

	for msgIndex, _ := range allData {

		lCount++

		fmt.Println("(process_input_data_redis_concurrent) count = "+strconv.Itoa(lCount)+" loading this data to redis db 0: key -> ", msgIndex, " map -> ", allData[msgIndex])

		msgCount, errCount = write_binary_message_to_redis(msgCount, errCount, msgIndex, allData[msgIndex], conn)

		fmt.Println("(process_input_data_redis_concurrent) count = "+strconv.Itoa(lCount)+" loaded data into redis db 0. message count: ", msgCount, " errors: ", errCount)

	}

	logger("(process_input_data_redis_concurrent)", "completing worker task "+strconv.Itoa(taskCount))

	//allocate the messages among the available worker pods
	startSequence := 1
	endSequence := msgCount

	//Generate input file list and distribute among workers
	inputQueue := generate_input_sources(startSequence, endSequence) //Generate the total list of input files in the source dirs

	metadata := strings.Join(inputQueue, ",")
	logger(logFile, "(process_input_data_redis_concurrent) generated new workload metadata: "+metadata)

	//get the currently deployed worker pods in the producer pool
	workers, cnt := get_worker_pool(workerType, namespace)

	//delete the current work allocation table as it is now stale data
	delete_stale_allocation_data(workers)

	//Assign message workload to worker pods
	workAllocationMap := assign_message_workload_workers(workers, inputQueue, cnt)

	//Update the work allocation in a REDIS database
	workCount = update_work_allocation_table(workAllocationMap, workCount)

	//update this global
	scaleMax = workCount
	fmt.Println("setting scaleMax to: ", scaleMax, " = ", workCount)

	//it's a global variable being updated concurrently, so mutex lock ...
	mutex.Lock()
	taskCount++
	mutex.Unlock()

	logger("(process_input_data_redis_concurrent)", "completed worker task "+strconv.Itoa(taskCount))
	logger("(process_input_data_redis_concurrent)", "task count = "+strconv.Itoa(taskCount)+" numWorkers = "+strconv.Itoa(numWorkers))

	//please fix this!!
	if workCount == numWorkers {
		//delete (or move) all processed files in the relevant db (dbIndex)
		//from Redis to somewhere else once they've been processed
		fmt.Println("(process_input_data_redis_concurrent) purging processed redis data in db ", inputDBIndex)
		purgeProcessedRedis(conn, inputDBIndex)
	} else {
		fmt.Println("(process_input_data_redis_concurrent) not purging yet: taskCount: ", taskCount, " work count: ", workCount, " numWorkers: ", numWorkers)
	}

	logger("(process_input_data_redis_concurrent)", "leaving routing process input data redis concurrent")
	logger("(process_input_data_redis_concurrent)", "task count = "+strconv.Itoa(taskCount)+" workcount = "+strconv.Itoa(workCount)+" numWorkers = "+strconv.Itoa(numWorkers))

}

func worker(id int, jobs <-chan int, results chan<- int, inputDBIndex int) {
	for j := range jobs {
		process_input_data_redis_concurrent(id, j, inputDBIndex)
		results <- numJobs //* numWorkers
	}
}

func read_input_sources_redis(sourceDBIndex int) (inputs []string) {

	var inputQueue []string

	//Get from Redis
	conn, err := redis.Dial("tcp", redisReadConnectionAddress)

	if err != nil {
		fmt.Println("(read_input_sources_redis) error dialing redis connection: ", err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		fmt.Println("(read_input_sources_redis) PANIC redis auth response: ", response)
	} else {
		fmt.Println("(read_input_sources_redis) OK redis auth response: ", response)
	}

	//GET ALL VALUES FROM DISCOVERED KEYS ONLY
	//select correct DB for this source
	fmt.Println("(read_input_sources_redis) select redis db: ", sourceDBIndex)
	conn.Do("SELECT", 6)

	defer conn.Close()

	//data, err := redis.Strings(conn.Do("KEYS", "*"))
	data, err := redis.Strings(conn.Do("KEYS", "*"))

	fmt.Println("(read_input_sources_redis) read items from redis db length: ", len(data))
	fmt.Println("(read_input_sources_redis) read items from redis db: content", data)

	if err != nil {
		log.Fatal(err)
	}

	for _, msgID := range data {

		inputQueue = append(inputQueue, msgID)

	}

	//revert to the default DB index (0)
	fmt.Println("(read_input_sources_redis) select default redis db: ", 0)
	conn.Do("SELECT", 0)

	//return the list of messages that exist in redis ...
	return inputQueue
}

func loadIdAllocator(taskMap map[int][]string, numWorkers int) (m map[string][]string) {

	fmt.Println("(loadIdAllocator) begin ...")

	tMap := make(map[string][]string)

	element := 0

	for i := 1; i <= numWorkers; i++ {

		taskID := strconv.Itoa(i)
		tMap[taskID] = taskMap[element]
		element++

	}

	fmt.Println("(loadIdAllocator) done ...", tMap)

	return tMap
}

func divide_and_conquer(inputs []string, numWorkers int, numJobs int) (m map[string][]string) {

	tempMap := make(map[int][]string)

	size := len(inputs) / numWorkers

	var j int
	var lc int = 0

	for i := 0; i < len(inputs); i += size {

		j += size

		if j > len(inputs) {
			j = len(inputs)
		}

		//just populate the map for now
		//later assign worker-job pairs
		tempMap[lc] = inputs[i:j]

		lc++

	}

	tMap := loadIdAllocator(tempMap, numWorkers)

	return tMap
}

func bulkOrderLoad(inputDBIndex int) {

	fmt.Println("(bulkOrderLoad) loading raw order data for processing from DB index ", inputDBIndex)

	inputQueue := read_input_sources_redis(inputDBIndex) //Alternatively, get the total list of input data files from redis instead

	taskMap = divide_and_conquer(inputQueue, numWorkers, numJobs) //get the allocation of workers to task-sets

	//debug
	fmt.Println("(bulkOrderLoad) generate taskmap: ", taskMap)

	//Making use of Go Worker pools for concurrency within a pod here ...
	jobs := make(chan int, numJobs)
	results := make(chan int, numJobs)

	for w := 1; w <= numWorkers; w++ {

		go worker(w, jobs, results, inputDBIndex)

	}

	for j := 1; j <= numJobs; j++ {

		jobs <- j

	}

	close(jobs)

	for r := 0; r <= numJobs*numWorkers; r++ {

		<-results

	}
}

func backupOrderQueue() (status string, err error) {

	status = "pending"

	logger(logFile, "(backupOrderQueue) backing up the order queue to redis ...")

	resp, err := http.Get(replayServiceAddress + "/streamer-backup")

	if err != nil {
		fmt.Println("(backupOrderQueue) http read error getting /streamer-backup", err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(backupOrderQueue) http read error reading backup service response body", err)
	}

	status = string(body)

	fmt.Println("(backupOrderQueue) Getting backup service status: ", status)

	//don't block on this ...
	go func() {
		resp, err = http.Get(replayServiceAddress + "/streamer-backup-status")

		if err != nil {
			log.Fatalln(err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(backupOrderQueue) http read error reading streamer-backup-status response body", err)
		}

		status = string(body)

		fmt.Println("(backupOrderQueue) triggered order queue backup start: ", status)

	}()

	for {
		resp, err = http.Get(replayServiceAddress + "/streamer-backup-status")

		if err != nil {
			fmt.Println("(backupOrderQueue) error getting /streamer-backup-status endpoint", err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(backupOrderQueue) error reading streamer-backup-status response body", err)
			break
		}

		status = string(body)

		fmt.Println("(backupOrderQueue) Getting backup service status: ", status)

		if status == "done" {
			fmt.Println("(backupOrderQueue) finished backing up order queue: ", status)
			break
		}
	}
	return status, err
}

func reloadPulsarQueue() {

	status := "pending"

	//[x] 0. Read replay status: GET /streamer-status
	resp, err := http.Get(replayServiceAddress + "/streamer-status")

	if err != nil {
		fmt.Println("(reloadPulsarQueue) http read error getting /streamer-status", err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(reloadPulsarQueue) http read error reading response body", err)
	}

	status = string(body)

	fmt.Println("(reloadPulsarQueue) Getting replay service status: ", status)

	//don't block on this ...
	go func() {

		resp, err = http.Get(replayServiceAddress + "/streamer-refresh")

		if err != nil {
			log.Fatalln(err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("( reloadPulsarQueue) http read error reading streamer-refresh response body", err)
		}

		status = string(body)
		fmt.Println("(reloadPulsarQueue) triggered replay start: ", status)

	}()

	for {
		resp, err = http.Get(replayServiceAddress + "/streamer-status")

		if err != nil {
			fmt.Println("(reloadPulsarQueue) error getting /streamer-status endpoint", err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(reloadPulsarQueue) error reading streamer-status response body", err)
			break
		}

		status = string(body)

		fmt.Println("(reloadPulsarQueue) Getting refresh service status: ", status)

		if status == "reloaded" {
			fmt.Println("(reloadPulsarQueue) finished replaying input data sequence: ", status)
			break
		}

		time.Sleep(5 * time.Second)
	}

	fmt.Println("(reloadPulsarQueue) done refreshing queuing system ...")

}

//END: Data loading code

func bootStrapOrderDataWrapper(sequenceReplayDBindex int) {

	//find current time, time 168 hours ago in ISO format
	msgStartTime, msgStopTime := getOrderInterval(dumpTimeInterval)

	serial := 0 //don't preserve the order sequence when using in load testing - just blast orders out randomly

	bootStrapOrderData(sequenceReplayDBindex, msgStartTime, msgStopTime, serial)
}

func bootStrapOrderData(sequenceReplayDBindex int, msgStartTime string, msgStopTime string, serial int) {

	//load the last 6 days of orders data from the kafka `api0001` topic
	//in preparation for any new load test (sequential order replay)
	//this is stored in REDIS DB 6

	fmt.Println("(bootStrapOrderData) done loading order data from kafka topic (api0001)...")

	//[x] 1. Trigger the loader service via the API (kubectl delete <pod-name>)
	// GET /load-start
	//http.Get(replayServiceAddress + "/streamer-start?start=" + strconv.Itoa(msgStartSeq) + "?stop=" + strconv.Itoa(msgStopSeq))
	resp, err := http.Get(orderDumperServiceAddress + "/dumper-start?start=" + msgStartTime + "?stop=" + msgStopTime)

	//debug help
	fmt.Println("(bootStrapOrderData) GET: ", orderDumperServiceAddress+"/dumper-start?start="+msgStartTime+"?stop="+msgStopTime)
	fmt.Println("(bootStrapOrderData) streaming orders from " + msgStartTime + " to " + msgStopTime)

	if err != nil {
		fmt.Println("error connecting to order dumper service", orderDumperServiceAddress)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("(bootStrapOrderData) error reading ingest status endpoint: ", orderDumperServiceAddress)
	}

	status := string(body)

	fmt.Println("(bootStrapOrderData) triggered order dump start: ", status)

	//	[x] 2. Wait until it's status "ok" (until /ingest-status/ is not `pending`)
	//	  [x] 2.1. Poll the status endpoint ("API": curl /load-status/) pending|started|completed

	for {

		resp, err = http.Get(orderDumperServiceAddress + "/dumper-status")

		if err != nil {
			fmt.Println("(bootStrapOrderData) error getting order dumper status endpoint: ", err)
		}

		body, err = ioutil.ReadAll(resp.Body)

		if err != nil {
			fmt.Println("(bootStrapOrderData) error reading order dumper status response body: ", err)
			break
		}

		status = string(body)

		fmt.Println("(bootStrapOrderData) Getting order dumper service status: ", status)

		if status == "done" {
			fmt.Println("(bootStrapOrderData) finished order dumper input data: ", status)
			break
		} else {
			fmt.Println("(bootStrapOrderData) downloading order data from kafka: ", status)
		}

		time.Sleep(5 * time.Second)
	}

	//assign the new orders to worker (serial) or workers (concurrent)
	if serial == 0 {
		//don't preserve the sequence of orders as read from kafka
		//in the REDIS DB table
		allocateOrderDataConcurrent(sequenceReplayDBindex)
	}

	if serial == 1 {
		//Preserve the sequence of orders as read from kafka in REDIS work allocation table.
		allocateOrderDataSerial(sequenceReplayDBindex)
	}

}

func allocateOrderDataConcurrent(db int) {

	//allocate the newly dumped order data to workers for concurrent processing
	//(different from the serial processing which is only assigned to one worker)

	//Generate input file list and distribute among workers
	inputQueue := read_input_sources_redis(db)

	logger(logFile, "(allocateOrderDataConcurrent) generated new workload metadata length: "+strconv.Itoa(len(inputQueue)))

	metadata := strings.Join(inputQueue, ",")

	logger(logFile, "(allocateOrderDataConcurrent) generated new workload metadata: "+metadata)

	//Allocate workers to the input data
	workCount := 0
	namespace := "ragnarok"

	//get the currently deployed worker pods in the producer pool
	workers, cnt := get_worker_pool(workerType, namespace)

	//delete the current work allocation table as it is now stale data
	delete_stale_allocation_data(workers)

	logger(logFile, "(allocateOrderDataConcurrent) done deleting stale allocation data. Preparing to create work allocation map ...")

	//Assign message workload to worker pods
	workAllocationMap := assign_message_workload_workers(workers, inputQueue, cnt)

	fmt.Println("(allocateOrderDataConcurrent) got work allocation map:", workAllocationMap)

	//Update the work allocation in a REDIS database
	workCount = update_work_allocation_table(workAllocationMap, workCount)

	//update this global
	scaleMax = workCount

}

func allocateOrderDataSerial(db int) {

	//allocate the newly dumped order data to workers for concurrent processing
	//(different from the serial processing which is only assigned to one worker)

	//Generate input file list and distribute among workers
	inputQueue := read_input_sources_redis(db)

	metadata := strings.Join(inputQueue, ",")

	logger(logFile, "(allocateOrderData) generated new workload metadata: "+metadata)

	//Allocate workers to the input data
	workCount := 0
	namespace := "ragnarok"

	//get the currently deployed worker pods in the producer pool
	workers, cnt := get_worker_pool(workerType, namespace)

	//delete the current work allocation table as it is now stale data
	delete_stale_allocation_data(workers)

	logger(logFile, "(allocateOrderData) done deleting stale allocation data. Preparing to create work allocation map ...")

	//Assign message workload to worker pods
	workAllocationMap := assign_message_workload_workers(workers, inputQueue, cnt)

	//Update the work allocation in a REDIS database
	workCount = update_work_allocation_table(workAllocationMap, workCount)

	//update this global
	scaleMax = workCount

}

func getOrderInterval(dumpTimeInterval int) (string, string) {

	//get start time and end time of order query from kafka (7 days duration)

	/*
		1. get current time
		2. delete 7 days from it
		3. print the start time (past)
		4. print the start time (current time)
		Expected time format conversions:
			st := "2022-05-26T10:50:00.000Z"
			et := "2022-05-28T20:51:00.000Z"
	*/

	timeStart := time.Now() //.Format(time.RFC3339)
	rfcTimeStart := timeStart.Format(time.RFC3339)

	//Travel 7 days back in time (168 hours)
	rfcTimeEnd := timeStart.Add(-time.Hour * time.Duration(dumpTimeInterval)).Format(time.RFC3339) //168  hours = 7 days into the past ...

	t1, e1 := time.Parse(
		time.RFC3339,
		rfcTimeStart)

	if e1 != nil {
		fmt.Println(e1)
	}

	t2, e2 := time.Parse(
		time.RFC3339,
		rfcTimeEnd)

	if e2 != nil {
		fmt.Println(e2)
	}

	//Convert from RFC3339 formatted time to ISO8601
	rfcTimeStart = t1.Format("2006-01-02T15:04:05.000Z")
	rfcTimeEnd = t2.Format("2006-01-02T15:04:05.000Z")

	return rfcTimeStart, rfcTimeEnd

}

func flushStaleOrderData(sourceDBIndex int) error {

	//Get from Redis
	conn, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		fmt.Println("(flushStaleOrderData) error dialing redis master connection: ", err)
	}

	// Now authenticate
	response, err := conn.Do("AUTH", redisAuthPass)

	if err != nil {
		fmt.Println("(flushStaleOrderData) PANIC redis auth response: ", response)
	} else {
		fmt.Println("(flushStaleOrderData) OK redis auth response: ", response)
	}

	fmt.Println("(flushStaleOrderData) select redis db: ", sourceDBIndex)
	conn.Do("SELECT", sourceDBIndex)

	defer conn.Close()

	_, err = conn.Do("FLUSHDB")

	fmt.Println("(flushStaleOrderData) flushed redis order data db: ", sourceDBIndex)

	if err != nil {
		fmt.Println("(flushStaleOrderData) couldn't flush the db", sourceDBIndex, " ", err)
	}

	//revert to the default DB index (0)
	fmt.Println("(read_input_sources_redis) select default redis db: ", 0)
	conn.Do("SELECT", 0)

	//return the list of messages that exist in redis ...
	return err
}

func main() {

	//refresh status of all synthetic user credentials in REDIS DB (DBN index 14)
	markUserCredentialsUnused()

	//load the last 6 days of orders data from the kafka `api0001` topic
	//in preparation for any new load test (sequential order replay)
	//this is stored in REDIS DB 6
	//DON'T BLOCK!
	go func() {

		//flush any previous data as it's stale ???
		err := flushStaleOrderData(sequenceReplayDBindex)

		if err != nil {
			fmt.Println("(main) could not  flush the order dump db!", err)
		}

		bootStrapOrderDataWrapper(sequenceReplayDBindex)
	}()

	//set up the metrics and management endpoint
	//Prometheus metrics UI
	http.Handle("/metrics", promhttp.Handler())

	//Management UI for Load Data Management
	//Administrative Web Interface
	admin := newAdminPortal()
	http.HandleFunc("/loader-admin", admin.handler)
	http.HandleFunc("/selected", admin.selectionHandler)

	//serve static content
	staticHandler := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", staticHandler))

	err := http.ListenAndServe(port_specifier, nil)

	if err != nil {
		fmt.Println("Could not start the metrics endpoint: ", err)
	}

}
