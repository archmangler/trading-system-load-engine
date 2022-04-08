package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/apache/pulsar/pulsar-client-go/pulsar"
	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

//Redis data storage details
var redisAuthPass string = os.Getenv("REDIS_PASS")
var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var port_specifier string = ":" + os.Getenv("METRICS_PORT_NUMBER")         // port for metrics service to listen on

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

func main() {

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

	/*DEBUG:
	for k, v := range streamContent {
		fmt.Println("msg key = ", k, " msg content = ", v)
	}
	*/

	status, errorCount := loadSequenceData(streamContent)
	fmt.Println("loading status: ", status, errorCount)
}

