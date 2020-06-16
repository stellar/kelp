package plugins

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/stellar/kelp/api"
	"github.com/stellar/kelp/model"
)

const secondsInDay = 24 * 60 * 60

// sellTwapLevelProvider provides a fixed number of levels using a static percentage spread
type sellTwapLevelProvider struct {
	startPf                                               api.PriceFeed
	offset                                                rateOffset
	orderConstraints                                      *model.OrderConstraints
	dowFilter                                             [7]volumeFilter
	numHoursToSell                                        int
	parentBucketSizeSeconds                               int
	distributeSurplusOverRemainingIntervalsPercentCeiling float64
	exponentialSmoothingFactor                            float64
	minChildOrderSizePercentOfParent                      float64
}

// ensure it implements the LevelProvider interface
var _ api.LevelProvider = &sellTwapLevelProvider{}

// makeSellTwapLevelProvider is a factory method
func makeSellTwapLevelProvider(
	startPf api.PriceFeed,
	offset rateOffset,
	orderConstraints *model.OrderConstraints,
	dowFilter [7]volumeFilter,
	numHoursToSell int,
	parentBucketSizeSeconds int,
	distributeSurplusOverRemainingIntervalsPercentCeiling float64,
	exponentialSmoothingFactor float64,
	minChildOrderSizePercentOfParent float64,
) (api.LevelProvider, error) {
	if numHoursToSell <= 0 || numHoursToSell > 24 {
		return nil, fmt.Errorf("invalid number of hours to sell, expected 0 < numHoursToSell <= 24; was %d", numHoursToSell)
	}

	if parentBucketSizeSeconds <= 0 || parentBucketSizeSeconds > secondsInDay {
		return nil, fmt.Errorf("invalid value for parentBucketSizeSeconds, expected 0 < parentBucketSizeSeconds <= %d (secondsInDay); was %d", secondsInDay, parentBucketSizeSeconds)
	}

	if (secondsInDay % parentBucketSizeSeconds) != 0 {
		return nil, fmt.Errorf("parentBucketSizeSeconds needs to perfectly divide secondsInDay but it does not; secondsInDay is %d and parentBucketSizeSeconds was %d", secondsInDay, parentBucketSizeSeconds)
	}

	if distributeSurplusOverRemainingIntervalsPercentCeiling < 0.0 || distributeSurplusOverRemainingIntervalsPercentCeiling > 1.0 {
		return nil, fmt.Errorf("distributeSurplusOverRemainingIntervalsPercentCeiling is invalid, expected 0.0 <= distributeSurplusOverRemainingIntervalsPercentCeiling <= 1.0; was %.f", distributeSurplusOverRemainingIntervalsPercentCeiling)
	}

	if exponentialSmoothingFactor < 0.0 || exponentialSmoothingFactor > 1.0 {
		return nil, fmt.Errorf("exponentialSmoothingFactor is invalid, expected 0.0 <= exponentialSmoothingFactor <= 1.0; was %.f", exponentialSmoothingFactor)
	}

	if minChildOrderSizePercentOfParent < 0.0 || minChildOrderSizePercentOfParent > 1.0 {
		return nil, fmt.Errorf("minChildOrderSizePercentOfParent is invalid, expected 0.0 <= minChildOrderSizePercentOfParent <= 1.0; was %.f", exponentialSmoothingFactor)
	}

	for i, f := range dowFilter {
		if !f.isSellingBase() {
			return nil, fmt.Errorf("volume filter at index %d was not selling the base asset as expected: %s", i, f.configValue)
		}
	}

	return &sellTwapLevelProvider{
		startPf:                 startPf,
		offset:                  offset,
		orderConstraints:        orderConstraints,
		dowFilter:               dowFilter,
		numHoursToSell:          numHoursToSell,
		parentBucketSizeSeconds: parentBucketSizeSeconds,
		distributeSurplusOverRemainingIntervalsPercentCeiling: distributeSurplusOverRemainingIntervalsPercentCeiling,
		exponentialSmoothingFactor:                            exponentialSmoothingFactor,
		minChildOrderSizePercentOfParent:                      minChildOrderSizePercentOfParent,
	}, nil
}

type bucketInfo struct {
	ID             int64
	totalBuckets   int64
	now            time.Time
	secondsElapsed int64
	volFilter      volumeFilter
	dailyLimit     float64
}

// String is the Stringer method
func (b *bucketInfo) String() string {
	return fmt.Sprintf(
		"BucketInfo[ID=%d, totalBuckets=%d, now=%s (day=%s, secondsElapsed=%d), volFilter=%s, dailyLimit=%.8f]",
		b.ID,
		b.totalBuckets,
		b.now.Format(time.RFC3339),
		b.now.Weekday().String(),
		b.secondsElapsed,
		b.volFilter.String(),
		b.dailyLimit,
	)
}

// GetLevels impl.
func (p *sellTwapLevelProvider) GetLevels(maxAssetBase float64, maxAssetQuote float64) ([]api.Level, error) {
	now := time.Now().UTC()
	log.Printf("GetLevels, unix timestamp for 'now' in UTC = %d (%s)\n", now.Unix(), now)
	bucket, e := p.makeBucketInfo(now)
	if e != nil {
		return nil, fmt.Errorf("unable to make bucketInfo: %s", e)
	}
	log.Printf("bucketInfo for this update round: %s\n", bucket)

	return []api.Level{}, nil
}

func (p *sellTwapLevelProvider) makeBucketInfo(now time.Time) (*bucketInfo, error) {
	volumeFilter := p.dowFilter[now.Weekday()]

	dailyLimit, e := volumeFilter.mustGetBaseAssetCapInBaseUnits()
	if e != nil {
		return nil, fmt.Errorf("could not fetch base asset cap in base units: %s", e)
	}

	dayStartTime := floorDate(now)
	dayEndTime := ceilDate(now)
	secondsToday := dayEndTime.Unix() - dayStartTime.Unix()
	totalBuckets := int64(math.Ceil(float64(secondsToday) / float64(p.parentBucketSizeSeconds)))

	secondsElapsed := now.Unix() - dayStartTime.Unix()
	bucketIdx := secondsElapsed / int64(p.parentBucketSizeSeconds)

	return &bucketInfo{
		ID:             bucketIdx,
		totalBuckets:   totalBuckets,
		now:            now,
		secondsElapsed: secondsElapsed,
		volFilter:      volumeFilter,
		dailyLimit:     dailyLimit,
	}, nil
}

// GetFillHandlers impl
func (p *sellTwapLevelProvider) GetFillHandlers() ([]api.FillHandler, error) {
	return nil, nil
}

func floorDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func ceilDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
}