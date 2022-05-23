package main

//Ingest user data for consumers to login with

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
)

type Credential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserId   int    `json:"userId"`
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
	c["user_id"] = strconv.Itoa(cred.UserId)

	return c
}

func main() {

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

	}

}
