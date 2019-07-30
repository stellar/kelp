package backend

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/stellar/go/build"
	"github.com/stellar/go/clients/horizonclient"
	"github.com/stellar/go/keypair"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/stellar/kelp/gui/model2"
	"github.com/stellar/kelp/plugins"
	"github.com/stellar/kelp/support/kelpos"
	"github.com/stellar/kelp/support/networking"
	"github.com/stellar/kelp/support/toml"
	"github.com/stellar/kelp/trader"
)

const issuerSeed = "SANPCJHHXCPRN6IIZRBEQXS5M3L2LY7EYQLAVTYD56KL3V7ABO4I3ISZ"

var centralizedPricePrecisionOverride = int8(6)
var centralizedVolumePrecisionOverride = int8(1)
var centralizedMinBaseVolumeOverride = float64(30.0)
var centralizedMinQuoteVolumeOverride = float64(10.0)

func (s *APIServer) autogenerateBot(w http.ResponseWriter, r *http.Request) {
	kp, e := keypair.Random()
	if e != nil {
		s.writeError(w, fmt.Sprintf("error generating keypair: %s\n", e))
		return
	}

	// make and register bot, which places it in the initial bot state
	bot := model2.MakeAutogeneratedBot()
	e = s.kos.RegisterBot(bot)
	if e != nil {
		s.writeError(w, fmt.Sprintf("error registering bot: %s\n", e))
		return
	}

	_, e = s.kos.Blocking("mkdir", "mkdir -p "+s.configsDir)
	if e != nil {
		s.writeError(w, fmt.Sprintf("error running mkdir command for configsDir: %s\n", e))
		return
	}

	_, e = s.kos.Blocking("mkdir", "mkdir -p "+s.logsDir)
	if e != nil {
		s.writeError(w, fmt.Sprintf("error running mkdir command for logsDir: %s\n", e))
		return
	}

	filenamePair := bot.Filenames()
	sampleTrader := s.makeSampleTrader(kp.Seed())
	traderFilePath := fmt.Sprintf("%s/%s", s.configsDir, filenamePair.Trader)
	log.Printf("writing autogenerated bot config to file: %s\n", traderFilePath)
	e = toml.WriteFile(traderFilePath, sampleTrader)
	if e != nil {
		s.writeError(w, fmt.Sprintf("error writing trader toml file: %s\n", e))
		return
	}

	sampleBuysell := makeSampleBuysell()
	strategyFilePath := fmt.Sprintf("%s/%s", s.configsDir, filenamePair.Strategy)
	log.Printf("writing autogenerated strategy config to file: %s\n", strategyFilePath)
	e = toml.WriteFile(strategyFilePath, sampleBuysell)
	if e != nil {
		s.writeError(w, fmt.Sprintf("error writing strategy toml file: %s\n", e))
		return
	}

	// we only want to start initializing bot once it has been created, so we only advance state if everything is completed
	go func() {
		e := s.setupAccount(kp.Address(), kp.Seed(), bot.Name)
		if e != nil {
			log.Printf("error setting up account for bot '%s': %s\n", bot.Name, e)
			return
		}

		e = s.kos.AdvanceBotState(bot.Name, kelpos.InitState())
		if e != nil {
			log.Printf("error advancing bot state after setting up account for bot '%s': %s\n", bot.Name, e)
			return
		}
	}()

	botJson, e := json.Marshal(*bot)
	if e != nil {
		s.writeError(w, fmt.Sprintf("unable to serialize bot: %s\n", e))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(botJson)
}

func (s *APIServer) setupAccount(address string, signer string, botName string) error {
	_, e := s.checkFundAccount(address, botName)
	if e != nil {
		return fmt.Errorf("error checking and funding account: %s\n", e)
	}

	client := s.apiTestNetOld
	txn, e := build.Transaction(
		build.SourceAccount{AddressOrSeed: address},
		build.AutoSequence{SequenceProvider: client},
		build.TestNetwork,
		build.Trust("COUPON", "GBMMZMK2DC4FFP4CAI6KCVNCQ7WLO5A7DQU7EC7WGHRDQBZB763X4OQI"),
		build.Payment(
			build.Destination{AddressOrSeed: address},
			build.CreditAmount{Code: "COUPON", Issuer: "GBMMZMK2DC4FFP4CAI6KCVNCQ7WLO5A7DQU7EC7WGHRDQBZB763X4OQI", Amount: "1000.0"},
			build.SourceAccount{AddressOrSeed: "GBMMZMK2DC4FFP4CAI6KCVNCQ7WLO5A7DQU7EC7WGHRDQBZB763X4OQI"},
		),
	)
	if e != nil {
		return fmt.Errorf("cannot create trustline transaction for account %s for bot '%s': %s\n", address, botName, e)
	}

	txnS, e := txn.Sign(signer, issuerSeed)
	if e != nil {
		return fmt.Errorf("cannot sign trustline transaction for account %s for bot '%s': %s\n", address, botName, e)
	}

	txn64, e := txnS.Base64()
	if e != nil {
		return fmt.Errorf("cannot convert trustline transaction to base64 for account %s for bot '%s': %s\n", address, botName, e)
	}

	resp, e := client.SubmitTransaction(txn64)
	if e != nil {
		return fmt.Errorf("error submitting change trust transaction for address %s for bot '%s': %s\n", address, botName, e)
	}

	log.Printf("successfully added trustline for address %s for bot '%s': %v\n", address, botName, resp)
	return nil
}

func (s *APIServer) checkFundAccount(address string, botName string) (*hProtocol.Account, error) {
	account, e := s.apiTestNet.AccountDetail(horizonclient.AccountRequest{AccountID: address})
	if e == nil {
		log.Printf("account already exists %s for bot '%s', no need to fund via friendbot\n", address, botName)
		return &account, nil
	} else if e != nil {
		var herr *horizonclient.Error
		switch t := e.(type) {
		case *horizonclient.Error:
			herr = t
		case horizonclient.Error:
			herr = &t
		default:
			return nil, fmt.Errorf("unexpected error when checking for existence of account %s for bot '%s': %s", address, botName, e)
		}

		if herr.Problem.Status != 404 {
			return nil, fmt.Errorf("unexpected horizon error code when checking for existence of account %s for bot '%s': %d (%v)", address, botName, herr.Problem.Status, *herr)
		}
	}

	// since it's a 404 we want to continue funding below
	var fundResponse interface{}
	e = networking.JSONRequest(http.DefaultClient, "GET", "https://friendbot.stellar.org/?addr="+address, "", nil, &fundResponse, "")
	if e != nil {
		return nil, fmt.Errorf("error funding address %s for bot '%s': %s\n", address, botName, e)
	}
	log.Printf("successfully funded account %s for bot '%s': %s\n", address, botName, fundResponse)
	return &account, nil
}

func (s *APIServer) makeSampleTrader(seed string) *trader.BotConfig {
	return trader.MakeBotConfig(
		"",
		seed,
		"XLM",
		"",
		"COUPON",
		"GBMMZMK2DC4FFP4CAI6KCVNCQ7WLO5A7DQU7EC7WGHRDQBZB763X4OQI",
		300,
		0,
		5,
		"both",
		0,
		0,
		s.apiTestNet.HorizonURL,
		nil,
		&trader.FeeConfig{
			CapacityTrigger: 0.8,
			Percentile:      90,
			MaxOpFeeStroops: 5000,
		},
		&centralizedPricePrecisionOverride,
		&centralizedVolumePrecisionOverride,
		&centralizedMinBaseVolumeOverride,
		&centralizedMinQuoteVolumeOverride,
	)
}

func makeSampleBuysell() *plugins.BuySellConfig {
	return plugins.MakeBuysellConfig(
		0.001,
		0.001,
		0.0,
		0.0,
		true,
		10.0,
		"exchange",
		"kraken/XXLM/ZUSD",
		"fixed",
		"1.0",
		[]plugins.StaticLevel{
			plugins.StaticLevel{
				SPREAD: 0.0010,
				AMOUNT: 100.0,
			}, plugins.StaticLevel{
				SPREAD: 0.0015,
				AMOUNT: 100.0,
			}, plugins.StaticLevel{
				SPREAD: 0.0020,
				AMOUNT: 100.0,
			}, plugins.StaticLevel{
				SPREAD: 0.0025,
				AMOUNT: 100.0,
			},
		},
	)
}
