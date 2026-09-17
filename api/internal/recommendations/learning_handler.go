package recommendations

import (
	"net/http"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// DeviceDaySignalsJSON is one day's signals derived on a member's device.
type DeviceDaySignalsJSON struct {
	Day                string  `json:"day"`
	Busyness           *string `json:"busyness,omitempty"`
	EveningFreeMinutes *int    `json:"eveningFreeMinutes,omitempty"`
	TemperatureBand    *string `json:"temperatureBand,omitempty"`
	Precipitation      *string `json:"precipitation,omitempty"`
}

// DeviceSignalsJSON are the device signals for a week.
type DeviceSignalsJSON struct {
	Days []DeviceDaySignalsJSON `json:"days"`
}

func (j *DeviceSignalsJSON) signals() (DeviceSignals, error) {
	var out DeviceSignals
	if j == nil {
		return out, nil
	}
	if len(j.Days) > 7 {
		return DeviceSignals{}, invalidf("signals.days must have at most 7 days")
	}
	for _, d := range j.Days {
		ds := DeviceDaySignals{Day: d.Day}
		if d.Busyness != nil {
			ds.Busyness = *d.Busyness
		}
		if d.EveningFreeMinutes != nil {
			if *d.EveningFreeMinutes <= 0 {
				return DeviceSignals{}, invalidf("signals.days.eveningFreeMinutes must be between 1 and %d", MaxEveningFreeMinutes)
			}
			ds.EveningFreeMinutes = *d.EveningFreeMinutes
		}
		if d.TemperatureBand != nil {
			ds.TemperatureBand = *d.TemperatureBand
		}
		if d.Precipitation != nil {
			ds.Precipitation = *d.Precipitation
		}
		out.Days = append(out.Days, ds)
	}
	return out, nil
}

func deviceSignalsJSON(s DeviceSignals) DeviceSignalsJSON {
	out := DeviceSignalsJSON{Days: []DeviceDaySignalsJSON{}}
	for _, d := range s.Days {
		dj := DeviceDaySignalsJSON{Day: d.Day, Busyness: optionalString(d.Busyness), EveningFreeMinutes: optionalInt(d.EveningFreeMinutes),
			TemperatureBand: optionalString(d.TemperatureBand), Precipitation: optionalString(d.Precipitation)}
		out.Days = append(out.Days, dj)
	}
	return out
}

// HolidayJSON is a holiday in a proposal's week.
type HolidayJSON struct {
	Day  string `json:"day"`
	Date string `json:"date"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ProposalContextJSON is the context a proposal was planned with.
type ProposalContextJSON struct {
	Season    *string           `json:"season"`
	OrderDate *string           `json:"orderDate"`
	Holidays  []HolidayJSON     `json:"holidays"`
	Signals   DeviceSignalsJSON `json:"signals"`
}

func proposalContextJSON(week string, first planning.Day, c ProposalContext) ProposalContextJSON {
	w, _ := planning.ParseWeek(week)
	out := ProposalContextJSON{
		Season: optionalString(c.Season), OrderDate: optionalString(c.OrderDate), Holidays: []HolidayJSON{},
		Signals: deviceSignalsJSON(c.Device),
	}
	for _, h := range c.Holidays {
		out.Holidays = append(out.Holidays, HolidayJSON{Day: h.Day, Date: w.DateOn(first, planning.Day(h.Day)), Name: h.Name, Kind: h.Kind})
	}
	return out
}

// LearnedAdjustmentJSON is one thing Autopilot learned.
type LearnedAdjustmentJSON struct {
	Kind     string  `json:"kind"`
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	RecipeID *string `json:"recipeId"`
	Value    float64 `json:"value"`
	// Direction is toward or away.
	Direction string `json:"direction"`
	Text      string `json:"text"`
	Evidence  int    `json:"evidence"`
}

// LearningResponse lists what Autopilot learned from the household's
// feedback.
type LearningResponse struct {
	ModelVersion string                  `json:"modelVersion"`
	Interactions int                     `json:"interactions"`
	Adjustments  []LearnedAdjustmentJSON `json:"adjustments"`
	ResetAt      *time.Time              `json:"resetAt"`
	ResetBy      *string                 `json:"resetBy"`
}

func newLearningResponse(l Learning) LearningResponse {
	resp := LearningResponse{
		ModelVersion: l.ModelVersion, Interactions: l.Interactions, Adjustments: []LearnedAdjustmentJSON{},
		ResetAt: optionalTime(l.ResetAt), ResetBy: optionalString(l.ResetBy),
	}
	for _, a := range l.Adjustments {
		direction := "toward"
		if a.Value < 0 {
			direction = "away"
		}
		resp.Adjustments = append(resp.Adjustments, LearnedAdjustmentJSON{
			Kind: a.Kind, Key: a.Key, Label: a.Label, RecipeID: optionalString(a.RecipeID), Value: a.Value,
			Direction: direction, Text: a.Text, Evidence: a.Evidence,
		})
	}
	return resp
}

func (h *Handler) getLearning(w http.ResponseWriter, r *http.Request) {
	l, err := h.opts.Service.Learning(r.Context(), actor(r).HouseholdID)
	if err != nil {
		h.writeError(w, r, "get autopilot learning failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newLearningResponse(l))
}

func (h *Handler) resetLearning(w http.ResponseWriter, r *http.Request) {
	m := actor(r)
	l, err := h.opts.Service.ResetLearning(r.Context(), m.HouseholdID, m.UserID)
	if err != nil {
		h.writeError(w, r, "reset autopilot learning failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newLearningResponse(l))
}
