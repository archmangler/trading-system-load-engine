package main

//Ingest user data for consumers to login with

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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

func readUserCredentialFile(uf string) []byte {

	// Open our jsonFile
	jsonFile, err := os.Open(uf)

	// if we os.Open returns an error then handle it
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("reading ", uf)
	// defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	return byteValue

}

func getUserCredentials(bv []byte) (c map[string]string) {

	c = make(map[string]string)

	var cred Credential

	err := json.Unmarshal(bv, &cred)

	if err != nil {
		log.Println(err)
	}

	c["username"] = cred.Email
	c["password"] = cred.Password
	c["userid"] = strconv.Itoa(cred.UserId)

	return c
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
	userid := temp["userid"]

	//REDIFY
	used := 0
	msgcnt := strconv.Itoa(msgIndex)

	//select correct DB (0)
	conn.Do("SELECT", credentialsDBindex)

	fmt.Println("debug> writing: ", "HMSET=", msgcnt, " username =", username, " password =", password, " userid =", userid, " used =", used)

	_, err := conn.Do("HMSET", msgIndex, "username", username, "password", password, "userid", userid, "used", used)

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

func updateCredentialParameter(credentialIndex int, usedStatus string, connw redis.Conn) (err error) {

	fmt.Println("(updateCredentialParameter) calling redis single field update with value: ", usedStatus)

	updateErr := update_parameter_to_redis(credentialIndex, usedStatus, connw)

	fmt.Println("(updateCredentialParameter) update error response:", updateErr)

	return updateErr
}

func update_parameter_to_redis(credentialIndex int, usedStatus string, connw redis.Conn) (err error) {

	fmt.Println("(update_parameter_to_redis) will update in redis: " + strconv.Itoa(credentialIndex))

	//select correct DB (0)
	fmt.Println("(update_parameter_to_redis) switching DB index")
	connw.Do("SELECT", credentialsDBindex)
	fmt.Println("(update_parameter_to_redis) setting required parameter value: " + usedStatus)

	//Testing.
	//used, _ := strconv.Atoi(usedStatus)

	_, err = connw.Do("HSET", strconv.Itoa(credentialIndex), "used", 1)

	if err != nil {
		fmt.Println("(update_parameter_to_redis) WARNING: error updating entry in redis " + strconv.Itoa(credentialIndex))
	} else {
		fmt.Println("(update_parameter_to_redis) successfuly updated entry in redis " + strconv.Itoa(credentialIndex))
	}

	fmt.Println("(update_parameter_to_redis) returning error response.")

	return err

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
		return msgPayload, err
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

func getCredentialbyIndex(idx int) (u string, p string, i int) {

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
	data, err := redis.Strings(connr.Do("KEYS", "*"))

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("(getCredentialbyIndex) got keys: ", data)

	d, _ := readFromRedis(idx, connr)

	fmt.Println("(getCredentialbyIndex) raw: ", d)

	u = d["username"]
	p = d["password"]

	i, _ = strconv.Atoi(d["userid"])

	s := d["used"]

	fmt.Println("(getCredentialbyIndex) credential id ", idx, " has status: ", s)

	//optimisation suggestion
	//for _, msgID := range data {
	//	inputQueue = append(inputQueue, msgID)
	//}

	//return the list of messages that exist in redis ...
	//fmt.Println("(getCredentialbyIndex) ", inputQueue)

	connr.Close()

	return u, p, i
}

func main() {

	var indices []int

	dataDir := "/app/development/pt_users/users/"

	files, err := ioutil.ReadDir(dataDir)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("ingesting user credentials ...")

	for _, file := range files {

		bv := readUserCredentialFile(dataDir + file.Name())

		credentials := getUserCredentials(bv)

		fmt.Println("credentials extracted: ", credentials)

		idx, msgs, errs := dump_credentials_to_input(credentials)

		fmt.Println("wrote: ", msgs, " err count: ", errs)

		//lookup the credential again
		u, p, i := getCredentialbyIndex(idx)
		fmt.Println("retrieved credentials: user=", u, " pass=", p, " id=", i)

		indices = append(indices, idx)

	}

	used := "1" //marked as "used" ("read")
	connw, err := redis.Dial("tcp", redisWriteConnectionAddress)

	if err != nil {
		fmt.Println("(updateCredentialParameter)redis connection response: ")
		//	log.Fatal(err)
	}

	// Now authenticate
	response, err := connw.Do("AUTH", redisAuthPass)

	if err != nil {
		fmt.Println("(updateCredentialParameter)redis auth response: ", response)
		panic(err)
	} else {
		fmt.Println("(updateCredentialParameter)redis auth response: ", response)
	}

	//Use defer to ensure the connection is always
	//properly closed before exiting the main() function.
	defer connw.Close()

	for i := range indices {
		fmt.Println("testing credential status update routine: ", indices[i])

		setErr := updateCredentialParameter(i, used, connw) //update the credential to indicate it has been retrieved and is likely in use.

		if setErr != nil {

			fmt.Println("(readFromRedis) WARNING: failed to update credential-in-use flag ...", setErr)

		}

	}

}

