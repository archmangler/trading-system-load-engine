package main

//Ingest user data for consumers to login with

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

var redisWriteConnectionAddress string = os.Getenv("REDIS_MASTER_ADDRESS") //address:port combination e.g  "my-release-redis-master.default.svc.cluster.local:6379"
var redisReadConnectionAddress string = os.Getenv("REDIS_REPLICA_ADDRESS") //address:port combination e.g  "my-release-redis-replicas.default.svc.cluster.local:6379"
var redisAuthPass string = os.Getenv("REDIS_PASS")
var credentialsDBindex int = 14

var msgIndex int = 0

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

func logger(logFile string, logMessage string) {

	now := time.Now()
	msgTs := now.UnixNano()
	fmt.Println("[log]", strconv.FormatInt(msgTs, 10), logMessage)
}

//2
func dump_credentials_to_input(users map[string]string) (msgIndex int, msgCount int, errCount int) {

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

	//Now pump data into redis
	msgIndex, errCount = write_message_to_redis(msgCount, errCount, users, conn)

	msgCount = msgIndex

	return msgIndex, msgCount, errCount
}

//1
func write_message_to_redis(msgCount int, errCount int, temp map[string]string, conn redis.Conn) (int, int) {

	logger("(write_message_to_redis)", "will write to redis: "+string(temp["username"]))

	username := temp["username"]
	password := temp["password"]
	user_id := temp["userid"]

	//REDIFY
	used := 0
	msgcnt := strconv.Itoa(msgIndex)

	//select correct DB (0)
	conn.Do("SELECT", 14)

	fmt.Println("debug> writing: ", "HMSET=", msgcnt, " username =", username, " password =", password, " userid =", user_id, " used =", used)

	_, err := conn.Do("HMSET", msgIndex, "username", username, "password", password, "userid", user_id, "used", used)

	if err != nil {

		logger("(write_message_to_redis)", "error writing message to redis: "+err.Error())
		//record as a failure metric
		errCount++

	} else {

		logger("(write_message_to_redis)", "wrote message to redis. count: "+strconv.Itoa(msgIndex))
		//record as a success metric
		msgIndex++

	}

	return msgIndex, errCount

}

func readFromRedis(input_id int, conn redis.Conn) (ds map[string]string, err error) {

	msgPayload := make(map[string]string)

	err = nil

	conn.Do("SELECT", 14)

	fmt.Println("getting index: ", input_id)

	values, err := redis.Values(conn.Do("HGETALL", input_id))

	if err != nil {
		fmt.Println([]byte(err.Error()))
	}

	fmt.Println("got this from HGETALL: ", values)

	p := User{}

	redis.ScanStruct(values, &p)

	fmt.Println("scanned into struct: ", p)

	username := p.Username
	password := p.Password
	user_id := p.Userid
	used := p.Used

	if used == "0" {

		fmt.Println("unused: ", used)

	} else {

		fmt.Println("used: ", used)

		return msgPayload, err

	}

	//We should marshall this json into a well defined struct but lets
	//take the shortcut for now ...
	//msgPayload = `[{ "msgId":"` + strconv.Itoa(input_id) + `","username":"` + username + `","password":"` + password + `","userid":"` + user_id + `","used":"` + used + `"}]`

	msgPayload["username"] = username
	msgPayload["password"] = password
	msgPayload["user_id"] = user_id
	msgPayload["used"] = used

	fmt.Println("debug -> ", msgPayload, " <- debug")

	//get all the required data for the input id and return as json string
	return msgPayload, err

}

func getCredentialbyIndex(idx int) (u string, p string, i int) {

	//lookup and return the credential
	var inputQueue []string

	//Get from Redis
	conn, err := redis.Dial("tcp", redisReadConnectionAddress)

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

	defer conn.Close()

	//select correct DB (0)
	conn.Do("SELECT", 14)

	//GET ALL VALUES FROM DISCOVERED KEYS ONLY
	data, err := redis.Strings(conn.Do("KEYS", "*"))

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("(getCredentialbyIndex) got keys: ", data)

	d, _ := readFromRedis(idx, conn)

	fmt.Println("(getCredentialbyIndex) raw: ", d)

	//optimisation suggestion
	for _, msgID := range data {
		inputQueue = append(inputQueue, msgID)
	}

	//return the list of messages that exist in redis ...
	fmt.Println("(getCredentialbyIndex) ", inputQueue)

	return u, p, i
}

func main() {

	idx := 0
	//lookup the credential again
	u, p, i := getCredentialbyIndex(idx)
	fmt.Println("retrieved credentials: user=", u, " pass=", p, " id=", i)

	credentials := make(map[string]string)

	credentials["username"] = "someuser2"
	credentials["password"] = "somepass2"
	credentials["userid"] = "666666"

	idx, msgs, errs := dump_credentials_to_input(credentials)

	fmt.Println("wrote index: ", idx, " messages: ", msgs, " err count: ", errs)

	//lookup the credential again
	u, p, i = getCredentialbyIndex(idx)
	fmt.Println("retrieved credentials: user=", u, " pass=", p, " id=", i)

	credentials["username"] = "someuser1"
	credentials["password"] = "somepass1"
	credentials["userid"] = "someid1"

	idx, msgs, errs = dump_credentials_to_input(credentials)

	fmt.Println("wrote index: ", idx, " messages: ", msgs, " err count: ", errs)

	//lookup the credential again
	u, p, i = getCredentialbyIndex(idx)
	fmt.Println("retrieved credentials: user=", u, " pass=", p, " id=", i)

}
