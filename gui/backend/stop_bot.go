package backend

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/stellar/kelp/support/kelpos"
)

func (s *APIServer) stopBot(w http.ResponseWriter, r *http.Request) {
	botNameBytes, e := ioutil.ReadAll(r.Body)
	if e != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("error when reading request input: %s\n", e)))
		return
	}
	botName := string(botNameBytes)

	e = s.kos.AdvanceBotState(botName, kelpos.BotStateRunning)
	if e != nil {
		log.Printf("error advancing bot state: %s\n", e)
		w.WriteHeader(http.StatusInternalServerError)
	}

	e = s.kos.Stop(botName)
	if e != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("error when killing bot %s: %s\n", botName, e)))
		return
	}
	log.Printf("stopped bot '%s'\n", botName)

	var numIterations uint8 = 1
	e = s.doStartBot(botName, "delete", &numIterations, func() {
		s.deleteFinishCallback(botName)
	})
	if e != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("error when deleting bot ortders %s: %s\n", botName, e)))
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *APIServer) deleteFinishCallback(botName string) {
	log.Printf("deleted offers for bot '%s'\n", botName)

	e := s.kos.AdvanceBotState(botName, kelpos.BotStateStopping)
	if e != nil {
		log.Printf("error advancing bot state: %s\n", e)
	}
}
