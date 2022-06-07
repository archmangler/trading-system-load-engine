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

func updateCredentialParameter(credentialIndex int, usedStatus int, connw redis.Conn) (err error) {

	fmt.Println("(updateCredentialParameter) calling redis single field update with value: ", usedStatus)

	updateErr := update_parameter_to_redis(credentialIndex, usedStatus, connw)

	fmt.Println("(updateCredentialParameter) update error response:", updateErr)

	return updateErr
}

func update_parameter_to_redis(credentialIndex int, usedStatus int, connw redis.Conn) (err error) {

	fmt.Println("(update_parameter_to_redis) will update in redis: " + strconv.Itoa(credentialIndex))

	//select correct DB (0)
	fmt.Println("(update_parameter_to_redis) switching DB index")
	connw.Do("SELECT", credentialsDBindex)
	fmt.Println("(update_parameter_to_redis) setting required parameter value: ", usedStatus)

	//Testing.
	//used, _ := strconv.Atoi(usedStatus)

	_, err = connw.Do("HSET", strconv.Itoa(credentialIndex), "used", usedStatus)

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

func getCredentialbyIndex(idx int) (u string, p string, i int, s string) {

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
		fmt.Println("(getCredentialbyIndex) redis auth response: ", response)
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

	fmt.Println("(getCredentialbyIndex) username ", u)

	p = d["password"]

	i, _ = strconv.Atoi(d["userid"])

	s = d["used"]

	fmt.Println("(getCredentialbyIndex) credential id ", idx, " has status: ", s)

	connr.Close()

	fmt.Println("(getCredentialbyIndex) returning 4 values: ", u, p, i, s)

	return u, p, i, s
}

func getAllunused() (hKeys []string) {

	//1. connect in read mode to db 14
	//2. collect all indexes in a slice
	//3. Return the slice

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
	hKeys, err = redis.Strings(connr.Do("KEYS", "*"))

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("(getCredentialbyIndex) got keys: ", hKeys)

	return hKeys
}

func main() {

	//lookup the credentials in the credentials table
	indices := getAllunused() //get all unused entries in the table

	var selected int = 0

	for idx := range indices {

		u, p, i, s := getCredentialbyIndex(idx)

		fmt.Println("(main) got currently unused credentials: ", "db index = ", idx, " u = ", u, " p = ", p, " i = ", i, " s = ", s)

		if s == "1" {

			fmt.Println("(main) user id: ", i, " is USED -> ", s)

		} else {
			fmt.Println("(main) user id: ", i, " is FREE -> ", s)
			selected = idx
			//pick a random one and jump out of the loop
			fmt.Println("(main) will use credential index: ", selected, " uid: ", i)
			break
		}
	}

	//connect to the write master of the redis cluster
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

	used := 1 //marked as "used" ("read")

	setErr := updateCredentialParameter(selected, used, connw) //update the credential to indicate it has been retrieved and is likely in use.

	if setErr != nil {
		fmt.Println("(readFromRedis) WARNING: failed to update credential-in-use flag ...", setErr)
	}

}
