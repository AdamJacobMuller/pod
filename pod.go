package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/influxdata/influxdb/client/v2"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	UserID  string    `json:"userId"`
	Email   string    `json:"email"`
	Expires time.Time `json"expires"`
	Token   string    `json:"token"`
}

type FullResponse struct {
	Pets []*Pet `json:"pets"`
}

type Pet struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Pod      Pod      `json:"pod"`
	Location Location `json:"location"`
}

type Pod struct {
	Battery Battery `json:"batteryInfo"`
}

type Battery struct {
	Status    string `json:"status"`
	Value     int    `json:"value"`
	Remaining int    `json:"remaining"`
}

type Location struct {
	Timestamp time.Time `json:"timestamp"`
	Latitude  float64   `json:"lat"`
	Longitude float64   `json:"lon"`
	Accuracy  float64   `json:"accuracy"`
}

func marshalGetUnmarshal(url string, request interface{}, response interface{}) error {
	jsonBody, err := json.Marshal(request)
	if err != nil {
		return err
	}
	buffer := bytes.NewBuffer(jsonBody)
	httprequest, err := http.NewRequest("POST", url, buffer)
	if err != nil {
		return err
	}
	httprequest.Header.Set("content-type", "application/json")
	httprequest.Header.Set("accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(httprequest)
	if err != nil {
		return err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()

	err = json.Unmarshal(body, response)
	if err != nil {
		return err
	}
	return nil
}

func getUnmarshalAuth(url string, authorization string, response interface{}) error {
	httprequest, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	httprequest.Header.Set("content-type", "application/json")
	httprequest.Header.Set("accept", "application/json")
	httprequest.Header.Set("authorization", authorization)
	client := &http.Client{}
	resp, err := client.Do(httprequest)
	if err != nil {
		return err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()

	err = json.Unmarshal(body, response)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	var err error

	email := os.Getenv("POD_EMAIL")
	password := os.Getenv("POD_PASSWORD")

	request := &LoginRequest{
		Email:    email,
		Password: password,
	}
	response := &LoginResponse{}

	marshalGetUnmarshal("https://api.podtrackers.net/pod/v3/authenticate/login", request, response)
	log.WithFields(log.Fields{
		"UserID":  response.UserID,
		"Email":   response.Email,
		"Expires": response.Expires,
	}).Info("got account")

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     os.Getenv("POD_INFLUX_ADDR"),
		Username: "",
		Password: "",
	})
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("influxdb NewHTTPClient creation failed")
	}

	var bp client.BatchPoints
	var pt *client.Point
	var pet *Pet
	var fullResponse *FullResponse
	var tags map[string]string
	var fields map[string]interface{}

	for _ = range time.Tick(time.Minute) {
		bp, err = client.NewBatchPoints(client.BatchPointsConfig{
			Database:  "pod",
			Precision: "s",
		})
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Fatal("influxdb NewBatchPoints creation failed")
		}

		fullResponse = &FullResponse{}
		err = getUnmarshalAuth("https://api.podtrackers.net/pod/v3/users/me/full", response.Token, fullResponse)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Fatal("full fetch failed")
		}
		log.WithFields(log.Fields{
			"pets": len(fullResponse.Pets),
		}).Info("fetched full data")

		//fmt.Printf("PETS> %+v\n", fullResponse)
		for _, pet = range fullResponse.Pets {
			//fmt.Printf("PET> %+v\n", pet)
			//fmt.Printf("PET>AGE> %s\n", time.Since(pet.Location.Timestamp))
			tags = map[string]string{
				"id":   pet.ID,
				"name": pet.Name,
			}
			fields = map[string]interface{}{
				"latitude":  pet.Location.Latitude,
				"longitude": pet.Location.Longitude,
				"accuracy":  pet.Location.Accuracy,
				"age":       time.Since(pet.Location.Timestamp).Seconds(),
			}

			pt, err = client.NewPoint("location", tags, fields, time.Now())
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Fatal("influxdb NewPoint creation failed")
			}
			bp.AddPoint(pt)

			tags = map[string]string{
				"id":   pet.ID,
				"name": pet.Name,
			}
			fields = map[string]interface{}{
				"value":     pet.Pod.Battery.Value,
				"remaining": pet.Pod.Battery.Remaining,
			}

			pt, err = client.NewPoint("battery", tags, fields, time.Now())
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Fatal("influxdb NewPoint creation failed")
			}
			bp.AddPoint(pt)
		}
		err = c.Write(bp)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Fatal("influxdb Write failed")
		}
		log.WithFields(log.Fields{}).Info("wrote data")
	}
}
