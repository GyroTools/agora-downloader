package agora

import (
	agoraConn "github.com/GyroTools/gtagora-connector-go/agora"
)

// connect returns a connected Agora client, either using apiKey directly or,
// if apiKey is empty, by exchanging username/password for one.
func connect(url, apiKey, username, password string, verifyCert bool) (*agoraConn.Agora, error) {
	if apiKey != "" {
		return agoraConn.Create(url, apiKey, verifyCert)
	}
	return agoraConn.CreateWithPassword(url, username, password, verifyCert)
}
