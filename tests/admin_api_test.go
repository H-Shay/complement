package tests

import (
	"encoding/json"
	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/match"
	"github.com/matrix-org/complement/should"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestAdminApi(t *testing.T) {
	deployment := complement.Deploy(t, 1)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{IsAdmin: true})
	bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{})

	roomAlias := "sacrificial-room"
	res := alice.CreateRoom(t, map[string]interface{}{
		"visibility":      "public",
		"room_alias_name": roomAlias,
	})
	if res.StatusCode != 200 {
		t.Fatal("didn't create room", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading room id failed: %s", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("error parsing JSON")
	}
	roomId := result["room_id"].(string)
	bob.JoinRoom(t, roomId, []string{"hs1"})

	t.Run("parallel", func(t *testing.T) {
		t.Run("Can delete a room using v2 api and get delete status", func(t *testing.T) {
			t.Parallel()

			path := []string{"_synapse", "admin", "v2", "rooms", roomId}
			reqBody := map[string]interface{}{"block": true, "purge": true}
			deleteRes := alice.MustDo(t, "DELETE", path, client.WithJSONBody(t, reqBody))

			if deleteRes.StatusCode != 200 {
				t.Fatalf("delete request failed with status %s", deleteRes.Status)
			}
			deleteBody, err := io.ReadAll(deleteRes.Body)
			if err != nil {
				t.Fatalf("reading request body failed: %s", err)
			}
			var result map[string]interface{}
			if err := json.Unmarshal(deleteBody, &result); err != nil {
				t.Fatalf("error parsing JSON")
			}
			deleteId := result["delete_id"].(string)

			statusPath := []string{"_synapse", "admin", "v2", "rooms", "delete_status", deleteId}
			alice.Do(
				t,
				"GET",
				statusPath,
				client.WithRetryUntil(
					10*time.Second,
					func(res *http.Response) bool {
						_, err := should.MatchResponse(res, match.HTTPResponse{
							StatusCode: 200,
							JSON: []match.JSON{
								match.JSONKeyEqual("status", "complete"),
								match.JSONKeyEqual("shutdown_room", map[string]interface{}{
									"failed_to_kick_users": []string{},
									"kicked_users":         []string{alice.UserID, bob.UserID},
									"local_aliases":        []string{},
									"new_room_id":          nil}),
							},
						})
						if err != nil {
							t.Log(err)
							return false
						}
						return true
					},
				),
			)
			// also check status by room id - /v2/rooms/(?P<room_id>[^/]*)/delete_status
			statusPath2 := []string{"_synapse", "admin", "v2", "rooms", roomId, "delete_status"}
			alice.Do(
				t,
				"GET",
				statusPath2,
				client.WithRetryUntil(
					10*time.Second,
					func(res *http.Response) bool {
						_, err := should.MatchResponse(res, match.HTTPResponse{
							StatusCode: 200,
							JSON: []match.JSON{
								match.JSONKeyEqual("results", []interface{}{map[string]interface{}{
									"delete_id": deleteId,
									"status":    "complete",
									"shutdown_room": map[string]interface{}{
										"failed_to_kick_users": []string{},
										"kicked_users":         []string{alice.UserID, bob.UserID},
										"local_aliases":        []string{},
										"new_room_id":          nil},
								}}),
							}})
						if err != nil {
							t.Log(err)
							return false
						}
						return true
					},
				),
			)
		})
	})
}
