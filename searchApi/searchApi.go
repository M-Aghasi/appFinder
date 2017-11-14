package searchApi

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
)

const APPLE_SEARCH_API string = "https://itunes.apple.com/search?"
const APPLE_LOOKUP_API string = "https://itunes.apple.com/lookup?"

// This type is main representation of apple api result
type AppleSearchResponse struct {
	ResultCount int                  `json:"resultCount"`
	Results     []AppleSearchAppInfo `json:"results"`
}

// This type is entry representation of apple api result
type AppleSearchAppInfo struct {
	IosId  int64    `json:"trackId"`
	Name   string   `json:"trackName"`
	Icon   string   `json:"artworkUrl100"`
	Banner []string `json:"screenshotUrls"`

	IosScore      float32 `json:"averageUserRating"`
	IosFileSize   string  `json:"fileSizeBytes"`
	ContentRating string  `json:"contentAdvisoryRating"`
	IosMinOs      string  `json:"minimumOsVersion"`
	Publisher     string  `json:"sellerName"`
	IosPrice      float32 `json:"price"`
	Desc          string  `json:"description"`
	IosBundleName string  `json:"bundleId"`
	Category      string  `json:"primaryGenreName"`
	ReleaseDate   string  `json:"releaseDate"`

	IosUrl string `json:"trackViewUrl"`
}

func SearchApp(resChan chan []AppleSearchAppInfo, criteria string) {
	resChan <- searchApple(criteria)
}

func LookupApp(resChan chan []AppleSearchAppInfo, id string) {
	resChan <- lookupApple(id)
}

// Searches appStore by a term and returns results in channel
func searchApple(criteria string) []AppleSearchAppInfo {
	url := APPLE_SEARCH_API + "term=" + url.QueryEscape(criteria) + "&media=software" + "&limit=8"
	response, err := http.Get(url)
	if err != nil {
		log.Println("Error: The HTTP request failed with error: " + err.Error())
		return nil
	}

	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Println("Error: Reading http response failed with error: " + err.Error())
		return nil
	}

	appleSearchAPiRes := AppleSearchResponse{}
	err = json.Unmarshal(responseData, &appleSearchAPiRes)
	if err != nil {
		log.Println("Error: Parsing http response as json failed with error: " + err.Error())
		return nil
	}

	var res = make([]AppleSearchAppInfo, appleSearchAPiRes.ResultCount)
	for i := 0; i < appleSearchAPiRes.ResultCount && i < 8; i++ {
		res[i] = appleSearchAPiRes.Results[i]
	}
	return res
}

// Searches appStore by a appID and returns result in channel
func lookupApple(id string) []AppleSearchAppInfo {
	url := APPLE_LOOKUP_API + "id=" + url.QueryEscape(id) + "&media=software" + "&limit=1"
	response, err := http.Get(url)
	if err != nil {
		log.Println("Error: The HTTP request failed with error: " + err.Error())
		return nil
	}

	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Println("Error: Reading http response failed with error: " + err.Error())
		return nil
	}

	appleSearchAPiRes := AppleSearchResponse{}
	err = json.Unmarshal(responseData, &appleSearchAPiRes)
	if err != nil {
		log.Println("Error: Parsing http response as json failed with error: " + err.Error())
		return nil
	}
	if len(appleSearchAPiRes.Results) < 1 {
		log.Println("Error: No results found for id: " + id)
		return nil
	}
	return appleSearchAPiRes.Results
}
