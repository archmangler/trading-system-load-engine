package main

import (
	"fmt"
	"time"
)

/*
- name: ORDER_STREAM_START_TIME
value: "2022-05-26T10:50:00.000Z"
- name: ORDER_STREAM_END_TIME
value: "2022-05-28T20:51:00.000Z"
*/

func main() {

	//simple time and date operations

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

	//Travel 7 days back in time
	rfcTimeEnd := timeStart.Add(-time.Hour * 168).Format(time.RFC3339) //168  hours = 7 days into the past ...

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

	fmt.Println("Now	: ", rfcTimeStart)
	fmt.Println("Then	: ", rfcTimeEnd)

}
