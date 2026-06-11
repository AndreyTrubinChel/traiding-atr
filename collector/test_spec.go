package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	refreshToken := os.Getenv("BSC_REFRESH_TOKEN")

	resp, _ := http.PostForm("https://be.broker.ru/trade-api-keycloak/realms/tradeapi/protocol/openid-connect/token", map[string][]string{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"trade-api-write"},
	})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tokenResp)

	url := "https://be.broker.ru/trade-api-market-data-connector/api/v1/instruments?classCode=SPBFUT&ticker=BRU6"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	fmt.Println(string(body))
}