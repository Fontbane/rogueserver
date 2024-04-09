package api

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/Flashfyre/pokerogue-server/db"
	"github.com/Flashfyre/pokerogue-server/defs"
	"github.com/klauspost/compress/zstd"
)

const sessionSlotCount = 3

// /savedata/get - get save data
func (s *Server) handleSavedataGet(w http.ResponseWriter, r *http.Request) {
	uuid, err := getUuidFromRequest(r)
	if err != nil {
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	switch r.URL.Query().Get("datatype") {
	case "0": // System
		system, err := readSystemSaveData(uuid)
		if err != nil {
			httpError(w, r, err.Error(), http.StatusInternalServerError)
			return
		}

		compensations, err := db.FetchAndClaimAccountCompensations(uuid)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to fetch compensations: %s", err), http.StatusInternalServerError)
			return
		}

		for k, v := range compensations {
			typeKey := strconv.Itoa(k)
			system.VoucherCounts[typeKey] += v
		}

		saveJson, err := json.Marshal(system)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to marshal save to json: %s", err), http.StatusInternalServerError)
			return
		}

		w.Write(saveJson)
	case "1": // Session
		slotId, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to convert slot id: %s", err), http.StatusBadRequest)
			return
		}

		if slotId < 0 || slotId >= sessionSlotCount {
			httpError(w, r, fmt.Sprintf("slot id %d out of range", slotId), http.StatusBadRequest)
			return
		}

		session, err := readSessionSaveData(uuid, slotId)
		if err != nil {
			httpError(w, r, err.Error(), http.StatusInternalServerError)
			return
		}

		saveJson, err := json.Marshal(session)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to marshal save to json: %s", err), http.StatusInternalServerError)
			return
		}

		w.Write(saveJson)
	default:
		httpError(w, r, "invalid data type", http.StatusBadRequest)
		return
	}
}

// /savedata/update - update save data
func (s *Server) handleSavedataUpdate(w http.ResponseWriter, r *http.Request) {
	uuid, err := getUuidFromRequest(r)
	if err != nil {
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	err = db.UpdateAccountLastActivity(uuid)
	if err != nil {
		log.Print("failed to update account last activity")
	}

	hexUuid := hex.EncodeToString(uuid)

	switch r.URL.Query().Get("datatype") {
	case "0": // System
		var system defs.SystemSaveData
		err = json.NewDecoder(r.Body).Decode(&system)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to decode request body: %s", err), http.StatusBadRequest)
			return
		}

		if system.TrainerId == 0 && system.SecretId == 0 {
			httpError(w, r, "invalid system data", http.StatusInternalServerError)
			return
		}

		err = db.UpdateAccountStats(uuid, system.GameStats)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to update account stats: %s", err), http.StatusBadRequest)
			return
		}

		var gobBuffer bytes.Buffer
		err = gob.NewEncoder(&gobBuffer).Encode(system)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to serialize save: %s", err), http.StatusInternalServerError)
			return
		}

		zstdWriter, err := zstd.NewWriter(nil)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to create zstd writer, %s", err), http.StatusInternalServerError)
			return
		}

		compressed := zstdWriter.EncodeAll(gobBuffer.Bytes(), nil)

		err = os.MkdirAll("userdata/"+hexUuid, 0755)
		if err != nil && !os.IsExist(err) {
			httpError(w, r, fmt.Sprintf("failed to create userdata folder: %s", err), http.StatusInternalServerError)
			return
		}

		err = os.WriteFile("userdata/"+hexUuid+"/system.pzs", compressed, 0644)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to write save file: %s", err), http.StatusInternalServerError)
			return
		}

		db.DeleteClaimedAccountCompensations(uuid)
	case "1": // Session
		slotId, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to convert slot id: %s", err), http.StatusBadRequest)
			return
		}

		if slotId < 0 || slotId >= sessionSlotCount {
			httpError(w, r, fmt.Sprintf("slot id %d out of range", slotId), http.StatusBadRequest)
			return
		}

		fileName := "session"
		if slotId != 0 {
			fileName += strconv.Itoa(slotId)
		}

		var session defs.SessionSaveData
		err = json.NewDecoder(r.Body).Decode(&session)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to decode request body: %s", err), http.StatusBadRequest)
			return
		}

		var gobBuffer bytes.Buffer
		err = gob.NewEncoder(&gobBuffer).Encode(session)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to serialize save: %s", err), http.StatusInternalServerError)
			return
		}

		zstdWriter, err := zstd.NewWriter(nil)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to create zstd writer, %s", err), http.StatusInternalServerError)
			return
		}

		compressed := zstdWriter.EncodeAll(gobBuffer.Bytes(), nil)

		err = os.MkdirAll("userdata/"+hexUuid, 0755)
		if err != nil && !os.IsExist(err) {
			httpError(w, r, fmt.Sprintf("failed to create userdata folder: %s", err), http.StatusInternalServerError)
			return
		}

		err = os.WriteFile(fmt.Sprintf("userdata/%s/%s.pzs", hexUuid, fileName), compressed, 0644)
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to write save file: %s", err), http.StatusInternalServerError)
			return
		}
	default:
		httpError(w, r, "invalid data type", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// /savedata/delete - delete save data
func (s *Server) handleSavedataDelete(w http.ResponseWriter, r *http.Request) {
	uuid, err := getUuidFromRequest(r)
	if err != nil {
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	err = db.UpdateAccountLastActivity(uuid)
	if err != nil {
		log.Print("failed to update account last activity")
	}

	hexUuid := hex.EncodeToString(uuid)

	switch r.URL.Query().Get("datatype") {
	case "0": // System
		err := os.Remove("userdata/" + hexUuid + "/system.pzs")
		if err != nil && !os.IsNotExist(err) {
			httpError(w, r, fmt.Sprintf("failed to delete save file: %s", err), http.StatusInternalServerError)
			return
		}
	case "1": // Session
		slotId, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			httpError(w, r, fmt.Sprintf("failed to convert slot id: %s", err), http.StatusBadRequest)
			return
		}

		if slotId < 0 || slotId >= sessionSlotCount {
			httpError(w, r, fmt.Sprintf("slot id %d out of range", slotId), http.StatusBadRequest)
			return
		}

		fileName := "session"
		if slotId != 0 {
			fileName += strconv.Itoa(slotId)
		}

		err = os.Remove(fmt.Sprintf("userdata/%s/%s.pzs", hexUuid, fileName))
		if err != nil && !os.IsNotExist(err) {
			httpError(w, r, fmt.Sprintf("failed to delete save file: %s", err), http.StatusInternalServerError)
			return
		}
	default:
		httpError(w, r, "invalid data type", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type SavedataClearResponse struct {
	Success bool `json:"success"`
}

// /savedata/clear - mark session save data as cleared and delete
func (s *Server) handleSavedataClear(w http.ResponseWriter, r *http.Request) {
	uuid, err := getUuidFromRequest(r)
	if err != nil {
		httpError(w, r, err.Error(), http.StatusBadRequest)
		return
	}

	err = db.UpdateAccountLastActivity(uuid)
	if err != nil {
		log.Print("failed to update account last activity")
	}

	slotId, err := strconv.Atoi(r.URL.Query().Get("slot"))
	if err != nil {
		httpError(w, r, fmt.Sprintf("failed to convert slot id: %s", err), http.StatusBadRequest)
		return
	}

	if slotId < 0 || slotId >= sessionSlotCount {
		httpError(w, r, fmt.Sprintf("slot id %d out of range", slotId), http.StatusBadRequest)
		return
	}

	var session defs.SessionSaveData
	err = json.NewDecoder(r.Body).Decode(&session)
	if err != nil {
		httpError(w, r, fmt.Sprintf("failed to decode request body: %s", err), http.StatusBadRequest)
		return
	}

	sessionCompleted := validateSessionCompleted(session)
	newCompletion := false

	if session.GameMode == 3 && session.Seed == dailyRunSeed {
		waveCompleted := session.WaveIndex
		if !sessionCompleted {
			waveCompleted--
		}
		err = db.AddOrUpdateAccountDailyRun(uuid, session.Score, waveCompleted)
		if err != nil {
			log.Printf("failed to add or update daily run record: %s", err)
		}
	}

	if sessionCompleted {
		newCompletion, err = db.TryAddSeedCompletion(uuid, session.Seed, int(session.GameMode))
		if err != nil {
			log.Printf("failed to mark seed as completed: %s", err)
		}
	}

	response, err := json.Marshal(SavedataClearResponse{Success: newCompletion})
	if err != nil {
		httpError(w, r, fmt.Sprintf("failed to marshal response json: %s", err), http.StatusInternalServerError)
		return
	}

	fileName := "session"
	if slotId != 0 {
		fileName += strconv.Itoa(slotId)
	}

	err = os.Remove(fmt.Sprintf("userdata/%s/%s.pzs", hex.EncodeToString(uuid), fileName))
	if err != nil && !os.IsNotExist(err) {
		httpError(w, r, fmt.Sprintf("failed to delete save file: %s", err), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}
