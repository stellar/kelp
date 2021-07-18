package plugins

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
	"github.com/stellar/kelp/api"
	"github.com/stellar/kelp/model"
)

const (
	STREAM_TICKER_FMT = "%s@ticker"
	STREAM_BOOK_FMT   = "%s@depth"
	TTLTIME           = time.Second * 3 // ttl time in seconds
)

var (
	timeToWaitForFirstEvent = time.Second
)

var (
	ErrConversionWsMarketEvent       = errConversion{from: "interface", to: "*binance.WsMarketStatEvent"}
	ErrConversionWsPartialDepthEvent = errConversion{from: "interface", to: "*binance.WsPartialDepthEvent"}
)

type Subscriber func(symbol string, state *mapEvents) (*stream, error)
type errMissingSymbol struct {
	symbol string
}

func (err errMissingSymbol) Error() string {
	return fmt.Sprintf("Symbol %s is missing from exchange intizialization", err.symbol)
}

type errConversion struct {
	from string
	to   string
}

func (err errConversion) Error() string {
	return fmt.Sprintf("Error conversion from %s to %s", err.from, err.to)
}

type stream struct {
	doneC   chan struct{}
	stopC   chan struct{}
	cleanup func()
}

//Wait until the stream ends
func (s stream) Wait() {

	if s.doneC == nil {
		return
	}

	<-s.doneC
}

//Close the stream and cleanup any data
func (s stream) Close() {
	if s.stopC == nil {
		return
	}
	s.stopC <- struct{}{}
	s.stopC = nil

	if s.cleanup != nil {
		s.cleanup()
	}
}

//mapData... struct used to data from events and timestamp when they are cached
type mapData struct {
	data      interface{}
	createdAt time.Time
}

//isStatle... check if data it's stale
func isStale(data mapData, ttl time.Duration) bool {

	return time.Now().Sub(data.createdAt).Seconds() > ttl.Seconds()
}

//struct used to cache events
type mapEvents struct {
	data map[string]mapData
	mtx  *sync.RWMutex
}

//Set ... set value
func (m *mapEvents) Set(key string, data interface{}) {

	now := time.Now()

	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.data[key] = mapData{
		data:      data,
		createdAt: now,
	}

}

//Get ... get value
func (m *mapEvents) Get(key string) (mapData, bool) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	data, isData := m.data[key]

	return data, isData
}

//Del ... delete cached value
func (m *mapEvents) Del(key string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	delete(m.data, key)

}

// create new map for cache
func makeMapEvents() *mapEvents {
	return &mapEvents{
		data: make(map[string]mapData),
		mtx:  &sync.RWMutex{},
	}
}

//struct used to keep all cached data
type events struct {
	SymbolStats *mapEvents
	BookStats   *mapEvents
}

func createStateEvents() *events {
	events := &events{
		SymbolStats: makeMapEvents(),
		BookStats:   makeMapEvents(),
	}

	return events
}

// 24hr rolling window ticker statistics for a single symbol. These are NOT the statistics of the UTC day, but a 24hr rolling window for the previous 24hrs.
// Stream Name: <symbol>@ticker
// Update Speed: 1000ms
func subcribeTicker(symbol string, state *mapEvents) (*stream, error) {

	wsMarketStatHandler := func(ticker *binance.WsMarketStatEvent) {
		state.Set(symbol, ticker)
	}

	errHandler := func(err error) {
		log.Printf("Error WsMarketsStat for symbol %s: %v\n", symbol, err)
	}

	doneC, stopC, err := binance.WsMarketStatServe(symbol, wsMarketStatHandler, errHandler)

	if err != nil {
		return nil, err
	}

	keepConnection(doneC, func() {
		subcribeTicker(symbol, state)
	})

	return &stream{doneC: doneC, stopC: stopC, cleanup: func() {
		state.Del(symbol)
	}}, err

}

//restart Connection with ws
// Binance close each connection after 24 hours
func keepConnection(doneC chan struct{}, reconnect func()) {

	go func() {
		<-doneC
		reconnect()
	}()
}

// Top <levels> bids and asks, pushed every second. Valid <levels> are 5, 10, or 20.
// <symbol>@depth<levels>@100ms
// 100ms
func subcribeBook(symbol string, state *mapEvents) (*stream, error) {

	wsPartialDepthHandler := func(event *binance.WsPartialDepthEvent) {
		state.Set(symbol, event)
	}

	errHandler := func(err error) {
		log.Printf("Error WsPartialDepthServe for symbol %s: %v\n", symbol, err)
	}

	//Subscribe to highest level
	doneC, stopC, err := binance.WsPartialDepthServe100Ms(symbol, "20", wsPartialDepthHandler, errHandler)

	if err != nil {
		return nil, err
	}

	keepConnection(doneC, func() {
		subcribeBook(symbol, state)
	})

	return &stream{doneC: doneC, stopC: stopC, cleanup: func() {
		state.Del(symbol)
	}}, err

}

type binanceExchangeWs struct {
	events *events

	streams    map[string]*stream
	streamLock *sync.Mutex

	assetConverter model.AssetConverterInterface
	delimiter      string
}

// makeBinanceWs is a factory method to make an binance exchange over ws
func makeBinanceWs() (*binanceExchangeWs, error) {

	binance.WebsocketKeepalive = true

	events := createStateEvents()

	beWs := &binanceExchangeWs{
		events:         events,
		delimiter:      "",
		assetConverter: model.CcxtAssetConverter,
		streamLock:     &sync.Mutex{},
		streams:        make(map[string]*stream),
	}

	return beWs, nil
}

//getPrceision... get precision for float string
func getPrecision(floatStr string) int8 {

	strs := strings.Split(floatStr, ".")

	if len(strs) != 2 {
		log.Printf("could not get precision for float %s\n", floatStr)
		return 0
	}

	return int8(len(strs[1]))
}

//subscribeStream and wait for the first event
func (beWs *binanceExchangeWs) subscribeStream(symbol, format string, subscribe Subscriber) (mapData, error) {

	stream, err := subscribe(symbol, beWs.events.SymbolStats)

	if err != nil {
		return mapData{}, fmt.Errorf("error when subscribing for %s: %s", symbol, err)
	}

	//Store stream
	beWs.streamLock.Lock()
	beWs.streams[fmt.Sprintf(format, symbol)] = stream
	beWs.streamLock.Unlock()

	//Wait for binance to send events
	time.Sleep(timeToWaitForFirstEvent)

	data, isStream := beWs.events.SymbolStats.Get(symbol)

	//We couldn't subscribe for this pair
	if !isStream {
		return mapData{}, fmt.Errorf("error while fetching ticker price for trading pair %s", symbol)
	}

	return data, nil
}

// GetTickerPrice impl.
func (beWs *binanceExchangeWs) GetTickerPrice(pairs []model.TradingPair) (map[model.TradingPair]api.Ticker, error) {

	priceResult := map[model.TradingPair]api.Ticker{}
	for _, p := range pairs {

		symbol, err := p.ToString(beWs.assetConverter, beWs.delimiter)

		if err != nil {
			return nil, err
		}

		tickerData, isTicker := beWs.events.SymbolStats.Get(symbol)

		if !isTicker {
			tickerData, err = beWs.subscribeStream(symbol, STREAM_TICKER_FMT, subcribeTicker)

			if err != nil {
				return nil, err
			}

		}

		//Show how old is the ticker
		log.Printf("Ticker for %s is %d milliseconds old!\n", symbol, time.Now().Sub(tickerData.createdAt).Milliseconds())

		if isStale(tickerData, TTLTIME) {
			return nil, fmt.Errorf("ticker for %s symbols is older than %v", symbol, TTLTIME)
		}

		tickerI := tickerData.data

		//Convert to WsMarketStatEvent
		ticker, isOk := tickerI.(*binance.WsMarketStatEvent)

		if !isOk {
			return nil, ErrConversionWsMarketEvent
		}

		askPrice, e := strconv.ParseFloat(ticker.AskPrice, 64)
		if e != nil {
			return nil, fmt.Errorf("unable to correctly parse 'ask': %s", e)
		}
		bidPrice, e := strconv.ParseFloat(ticker.BidPrice, 64)
		if e != nil {
			return nil, fmt.Errorf("unable to correctly parse 'bid': %s", e)
		}
		lastPrice, e := strconv.ParseFloat(ticker.LastPrice, 64)
		if e != nil {
			return nil, fmt.Errorf("unable to correctly parse 'last': %s", e)
		}

		priceResult[p] = api.Ticker{
			AskPrice:  model.NumberFromFloat(askPrice, getPrecision(ticker.AskPrice)),
			BidPrice:  model.NumberFromFloat(bidPrice, getPrecision(ticker.BidPrice)),
			LastPrice: model.NumberFromFloat(lastPrice, getPrecision(ticker.LastPrice)),
		}
	}

	return priceResult, nil
}

//GetOrderBook impl
func (beWs *binanceExchangeWs) GetOrderBook(pair *model.TradingPair, maxCount int32) (*model.OrderBook, error) {

	var (
		maxCountInt = int(maxCount)
		fetchLimit  = maxCountInt
	)

	if fetchLimit > 20 {
		fetchLimit = 20
	}

	symbol, err := pair.ToString(beWs.assetConverter, beWs.delimiter)
	if err != nil {
		return nil, fmt.Errorf("error converting pair to string: %s", err)
	}

	bookData, isBook := beWs.events.BookStats.Get(symbol)

	if !isBook {

		bookData, err = beWs.subscribeStream(symbol, STREAM_BOOK_FMT, subcribeBook)

		if err != nil {
			return nil, err
		}

	}

	//Show how old is the ticker
	log.Printf("Ticker for %s is %d milliseconds old!\n", symbol, time.Now().Sub(bookData.createdAt).Milliseconds())

	if isStale(bookData, TTLTIME) {
		return nil, fmt.Errorf("ticker for %s symbols is older than %v", symbol, TTLTIME)
	}

	bookI := bookData.data

	//Convert to WsMarketStatEvent
	book, isOk := bookI.(*binance.WsPartialDepthEvent)

	if !isOk {
		return nil, ErrConversionWsPartialDepthEvent
	}

	askCcxtOrders := book.Asks
	bidCcxtOrders := book.Bids

	if fetchLimit != maxCountInt {
		// we may not have fetched all the requested levels because the exchange may not have had that many levels in depth
		if len(askCcxtOrders) > maxCountInt {
			askCcxtOrders = askCcxtOrders[:maxCountInt]
		}
		if len(bidCcxtOrders) > maxCountInt {
			bidCcxtOrders = bidCcxtOrders[:maxCountInt]
		}
	}

	asks, err := beWs.readOrders(askCcxtOrders, pair, model.OrderActionSell)

	if err != nil {
		return nil, err
	}

	bids, err := beWs.readOrders(bidCcxtOrders, pair, model.OrderActionBuy)

	if err != nil {
		return nil, err
	}

	return model.MakeOrderBook(pair, asks, bids), nil
}

//readOrders... transform orders from binance to model.Order
func (beWs *binanceExchangeWs) readOrders(orders []common.PriceLevel, pair *model.TradingPair, orderAction model.OrderAction) ([]model.Order, error) {

	pricePrecision := getPrecision(orders[0].Price)
	volumePrecision := getPrecision(orders[0].Quantity)

	result := []model.Order{}
	for _, o := range orders {

		price, quantity, err := o.Parse()

		if err != nil {
			return nil, err
		}

		result = append(result, model.Order{
			Pair:        pair,
			OrderAction: orderAction,
			OrderType:   model.OrderTypeLimit,
			Price:       model.NumberFromFloat(price, pricePrecision),
			Volume:      model.NumberFromFloat(quantity, volumePrecision),
			Timestamp:   nil,
		})
	}
	return result, nil
}

//Unsubscribe ... unsubscribe from binance streams
func (beWs *binanceExchangeWs) Unsubscribe(stream string) {

	beWs.streamLock.Lock()

	if stream, isStream := beWs.streams[stream]; isStream {
		stream.Close()
	}

	beWs.streamLock.Unlock()
}
