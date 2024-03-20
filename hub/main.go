package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"crypto/rand"
	"encoding/json"
	"bytes"
)

type subscriber struct {
	callbackURL string
	secret      string
	topic       string
}

var subscribers []subscriber
var verifiedSubscribers []subscriber

func getSubscriberRequest(w http.ResponseWriter, r *http.Request) {
	// Make sure that the method and URL path are correct.
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := readRequestBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bodyParsed, err := parseRequestBody(bodyBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	callbackURL := bodyParsed.Get("hub.callback")
	secret := bodyParsed.Get("hub.secret")
	topic := bodyParsed.Get("hub.topic")

	// Validate parsed data
	if callbackURL == "" || topic == "" {
		http.Error(w, "Missing callback URL or topic", http.StatusBadRequest)
		return
	}

	// Append to subscribers
	newSubscriber := subscriber{callbackURL: callbackURL, secret: secret, topic: topic}
	subscribers = append(subscribers, newSubscriber)

	// verify subscriber asynchronously
	go verifySubscriber(newSubscriber)

	log.Printf("Subscriber added: %+v", newSubscriber)
	fmt.Fprint(w, "Subscription request received.")
}

func verifySubscriber(sub subscriber) {
	log.Printf("Verifying subscriber: %s", sub.callbackURL)
	
	challenge, err := generateRandomString(16)
	if err != nil {
		log.Printf("Error generating challenge for verification: %v", err)
		return
	}

	values := url.Values{}
	values.Set("hub.mode", "subscribe")
	values.Set("hub.topic", sub.topic)
	values.Set("hub.challenge", challenge)

	verificationURL := fmt.Sprintf("%s?%s", sub.callbackURL, values.Encode())

	resp, err := http.Get(verificationURL)
	if err != nil {
		log.Printf("Error sending verification request to %s: %v", sub.callbackURL, err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response from subscriber %s: %v", sub.callbackURL, err)
		return
	}
	log.Println("BOOOODY", string(body))
	// The subscriber is supposed to echo back the challenge
	if string(body) != challenge {
		log.Printf("Verification failed for subscriber: %s", sub.callbackURL)
	} else {
		log.Printf("Subscriber verified: %s", sub.callbackURL)
		verifiedSubscribers = append(verifiedSubscribers, sub)
		log.Printf("VERIFIED SUBSCRIBERS", verifiedSubscribers)
	}
	
}

func generateRandomString(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func createSignature(secret string, message string) string{
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := mac.Sum(nil)
	
	return hex.EncodeToString(signature)
}

func readRequestBody(r *http.Request) ([]byte, error) {
	bodyBytes, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("can't read body: %v", err)
	}

	return bodyBytes, nil
}

func parseRequestBody(bodyBytes []byte) (url.Values, error) {
	bodyString := string(bodyBytes)
	bodyParsed, err := url.ParseQuery(bodyString)
	if err != nil {
		return nil, fmt.Errorf("can't parse body: %v", err)
	}

	return bodyParsed, nil
}

func initiateSubscriptionDance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

	resp, err := http.Get("http://web-sub-client:8080" +"/resub")
	if err != nil {
		log.Printf("Error initiating subscription dance: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Println("Subscription dance initiated successfully.")
	} else {
		log.Printf("Failed to initiate subscription dance, status code: %d", resp.StatusCode)
	}
}

func fetchSubscriberLogs() {
	resp, err := http.Get("http://web-sub-client:8080" + "/log")
	if err != nil {
		log.Printf("Error fetching subscriber logs: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading subscriber logs response body: %v", err)
		return
	}

	log.Printf("Subscriber logs:\n%s", string(body))
}

func publishContent(w http.ResponseWriter, r *http.Request) {
	// Check if the HTTP method is GET
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Define the JSON content
    data := map[string]string{
        "message": "New content available",
    }
    jsonData, err := json.Marshal(data)
    if err != nil {
        log.Printf("Error marshaling JSON: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    // Iterate over all verified subscribers and send them the signed content
    for _, sub := range verifiedSubscribers {
        signature := createSignature(sub.secret, string(jsonData))
        sendSignedContent(sub, jsonData, signature)
    }

    fmt.Fprintf(w, "Content published to %d verified subscribers.\n", len(verifiedSubscribers))
}

func sendSignedContent(sub subscriber, jsonData []byte, signature string) {
    client := &http.Client{}
    
	req, err := http.NewRequest("POST", sub.callbackURL, bytes.NewReader(jsonData))
    
	if err != nil {
        log.Printf("Failed to create request for subscriber %s: %v", sub.callbackURL, err)
        return
    }

    // Add the signature and Content-Type headers
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Hub-Signature", fmt.Sprintf("sha256=%s", signature))

    // Send the request
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("Error sending signed content to subscriber %s: %v", sub.callbackURL, err)
        return
    }
    defer resp.Body.Close()

    log.Printf("Signed content sent to subscriber %s, response status: %d", sub.callbackURL, resp.StatusCode)
	fetchSubscriberLogs()
}


func main() {
	http.HandleFunc("/", getSubscriberRequest)
	http.HandleFunc("/publish", publishContent)
	http.HandleFunc("/resub", initiateSubscriptionDance)
	

	port := "8080"
	log.Printf("Starting server on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
