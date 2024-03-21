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
	"sync"
)

type subscriber struct {
	callbackURL string
	secret      string
	topic       string
}
var verifiedSubscribersMutex sync.Mutex
var verifiedSubscribers []subscriber

func getSubscriberRequest(w http.ResponseWriter, r *http.Request) {
	// Make sure that the method and URL path are correct.
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyParsed, err := parseRequestBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	callbackURL := bodyParsed.Get("hub.callback")
	secret := bodyParsed.Get("hub.secret")
	topic := bodyParsed.Get("hub.topic")

	// Validate parsed data
	if callbackURL == "" || topic == "" || secret == "" {
		http.Error(w, "Missing subscriber data", http.StatusBadRequest)
		return
	}

	newSubscriber := subscriber{callbackURL: callbackURL, secret: secret, topic: topic}

	fmt.Fprint(w, "Subscription request received.")
	 
	verifySubscriber(newSubscriber)
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


	// The subscriber echos back the challenge
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response from subscriber %s: %v", sub.callbackURL, err)
		return
	}
	
	if string(body) != challenge {
		log.Printf("Verification failed for subscriber: %s", sub.callbackURL)
	} else {
		log.Printf("Subscriber verified: %s", sub.callbackURL)
		verifiedSubscribersMutex.Lock()
		verifiedSubscribers = append(verifiedSubscribers, sub)
		verifiedSubscribersMutex.Unlock()
	}
	
}

func parseRequestBody(r *http.Request) (url.Values, error) {
    // Read the body directly from the http.Request object
    bodyBytes, err := ioutil.ReadAll(r.Body)
    if err != nil {
        return nil, fmt.Errorf("can't read body: %v", err)
    }
    defer r.Body.Close()

    // Convert the body to a string and then parse it as URL-encoded data
	bodyString := string(bodyBytes)
    bodyParsed, err := url.ParseQuery(bodyString)

    if err != nil {
        return nil, fmt.Errorf("can't parse body: %v", err)
    }

    return bodyParsed, nil
}

func generateRandomString(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
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

	verifiedSubscribersMutex.Lock()

	verifiedSubsCopy:= make([]subscriber, len(verifiedSubscribers))
	copy(verifiedSubsCopy, verifiedSubscribers)
	
	verifiedSubscribersMutex.Unlock()

    // Iterate over all verified subscribers and send them the signed content
    for _, sub := range verifiedSubsCopy {
		go func (sub subscriber)  {
			signature := createSignature(sub.secret, string(jsonData))
			sendSignedContent(sub, jsonData, signature)
		}(sub)	
    }

    fmt.Fprintf(w, "Content published to %d verified subscribers.\n", len(verifiedSubsCopy))
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

func createSignature(secret string, message string) string{
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := mac.Sum(nil)
	
	return hex.EncodeToString(signature)
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
	log.Println("___________")
	log.Printf("Subscriber logs:\n%s", string(body))
	log.Println("___________")
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
