package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var debug int = 0 //debug switch: 1|0
var symbolLookupDataFile string = "./securities.csv"
var sourceDatafilePath string = "./in-msg.txt"

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

//for reference
type Order struct {
	InstrumentId  int    `json:"instrumentId"`
	Symbol        string `json:"symbol"`
	UserId        int    `json:"userId"`
	Side          int    `json:"side"`
	OrdType       int    `json:"ordType"`
	Price         int    `json:"price"`
	PriceScale    int    `json:"price_scale"`
	Quantity      int    `json:"quantity"`
	QuantityScale int    `json:"quantity_scale"`
	Nonce         int    `json:"nonce"`
	BlockWaitAck  int    `json:"blockWaitAck "`
	ClOrdId       string `json:"clOrdId"`
}

// readLines reads a whole file into memory
// and returns a slice of its lines.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
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

func processHistoricalOrderStream() {

	//We expect each line in the dump file will have the following format:
	//2022-05-31T01:57:45.652Z|400486014|NewOrderSingleRequest|{"headerTransactionId":0,"headerTransactionEnd":false,"securityId":52,"submitterId":37676,"account":37676,"price":38000,"priceScale":0,"qty":1,"qtyScale":0,"side":"SELL","orderId":0,"orderPriority":0,"secondaryOrderId":0,"clOrdId":"1653962265652380729","apiOrderID":"1653962265652380729-139c1a67-bd7b-4bcd-ab0c-4bb0782e0e6c","ordType":"LIMIT","type":0,"priceInt":0,"quantityLong":0,"quantityOrigLong":0,"quantityOrigScale":0,"timeInForce":"GOOD_TILL_CANCEL","expireTime":0,"stopPx":0,"stopPxScale":0,"stopPxInt":0,"toClose":false,"price2":0,"price2Scale":0,"price2Int":0,"marginCheckReferencePrice":0,"fillCumNotional":0,"origOrderId":0,"origClOrdId":null,"targetStrategy":0,"feeEstimatedQuantity":0,"feeAccumulatedQuantity":0,"availableEstimatedQuantity":0,"availableAccumulatedQuantity":0,"minMaxPrice":0,"returnedStack":null,"apiTimestamp":1653962265575,"pendingReplace":false,"liquidation":false,"lastLook":false,"hidden":false}

	theData, err := readLines(sourceDatafilePath)

	if err != nil {
		fmt.Println("(processHistoricalOrderStream) there was an error: ", err)
	}

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

		}
	}

}

func main() {
	processHistoricalOrderStream()
}
