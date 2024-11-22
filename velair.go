// Package velair provides utilities for interactive with
// Uflex Velar VSD Air Conditioners
package velair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is an HTTP interface for Velair air conditioners.
type Client struct {
	doer    Doer
	baseURL string
}

// Doer allows replacing the http client
// See https://pkg.go.dev/net/http#Client
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// FanSpeed of the unit.
// Some units do not support all speeds.
type FanSpeed int

const (
	FanSpeedAuto    FanSpeed = 0
	FanSpeedLow     FanSpeed = 1
	FanSpeedMedium  FanSpeed = 2
	FanSpeedHigh    FanSpeed = 3
	FanSpeedMaximum FanSpeed = 4
)

// FanSpeedFromInt convertes from an integer to FanSpeed
func FanSpeedFromInt(in int) (FanSpeed, error) {
	if in >= 0 && in <= 4 {
		return FanSpeed(in), nil
	}

	return -1, fmt.Errorf("invalid fan speed %d", in)
}

// String returns a user friendly representation.
func (f FanSpeed) String() string {
	switch f {
	case FanSpeedAuto:
		return "auto"
	case FanSpeedLow:
		return "low"
	case FanSpeedMedium:
		return "medium"
	case FanSpeedHigh:
		return "high"
	case FanSpeedMaximum:
		return "maximum"
	}

	return "unknown"
}

// DeviceMode is the mode of the unit.
// Not all units support all modes.
type DeviceMode int

const (
	DeviceModeHeating    DeviceMode = 0
	DeviceModeCooling    DeviceMode = 1
	DeviceModeDehumidify DeviceMode = 3
	DeviceModeFanOnly    DeviceMode = 4
	DeviceModeAuto       DeviceMode = 5
)

// DeviceModeFromInt
func DeviceModeFromInt(in int) (DeviceMode, error) {
	switch in {
	case 0:
		return DeviceModeHeating, nil
	case 1:
		return DeviceModeCooling, nil
	case 3:
		return DeviceModeDehumidify, nil
	case 4:
		return DeviceModeFanOnly, nil
	case 5:
		return DeviceModeAuto, nil
	}

	return -1, fmt.Errorf("invalid device mode %d", in)
}

// String returns a user friendly representation.
func (d DeviceMode) String() string {
	switch d {
	case DeviceModeHeating:
		return "heating"
	case DeviceModeCooling:
		return "cooling"
	case DeviceModeDehumidify:
		return "dehumidification"
	case DeviceModeFanOnly:
		return "fanonly"
	case DeviceModeAuto:
		return "auto"
	}

	return "unknown"
}

// DeviceStatus represents the current status of the air conditioning unit.
type DeviceStatus struct {
	Name        string
	FanSpeed    FanSpeed
	NightMode   bool
	Power       bool
	SetPoint    int // in Celsius
	Temperature int // in Celsius
	Mode        DeviceMode
}

type rawDeviceStatus struct {
	Success bool   `json:"success,omitempty"`
	Error   string `json:"error,omitempty"`
	Result  struct {
		FanSpeed    int `json:"fs"`
		NightMode   int `json:"nm"`
		Power       int `json:"ps"`
		SetPoint    int `json:"sp"`
		Temperature int `json:"t"`
		Mode        int `json:"wm"`
	} `json:"RESULT"`
	Setup struct {
		Name string `json:"name"`
	} `json:"setup"`
}

// GetStatus gets the current status of the unit.
func (c *Client) GetStatus(ctx context.Context) (*DeviceStatus, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v/1/status",
		nil,
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return ParseRawStatus(data)
}

// ParseRawStatus parses the raw status returned from the device
// to DeviceStatus
func ParseRawStatus(data []byte) (*DeviceStatus, error) {
	var raw rawDeviceStatus

	err := json.Unmarshal(data, &raw)
	if err != nil {
		return nil, err
	}

	if !raw.Success {
		if raw.Error != "" {
			return nil, fmt.Errorf("error from device %s", raw.Error)
		}

		return nil, errors.New("unsuccessful request but no error defined")
	}

	if raw.Error != "" {
		return nil, fmt.Errorf("error from device %s", raw.Error)
	}

	status := DeviceStatus{
		Name:        raw.Setup.Name,
		SetPoint:    raw.Result.SetPoint,
		Temperature: raw.Result.Temperature,
	}

	status.FanSpeed, err = FanSpeedFromInt(raw.Result.FanSpeed)
	if err != nil {
		return nil, err
	}

	status.Mode, err = DeviceModeFromInt(raw.Result.Mode)
	if err != nil {
		return nil, err
	}

	status.NightMode = raw.Result.NightMode == 1
	status.Power = raw.Result.Power == 1

	return &status, nil
}

func boolToStrInt(in bool) string {
	if in {
		return "1"
	}

	return "0"
}

// SetNightMode enable or disables night mode.
func (c *Client) SetNightMode(ctx context.Context, enable bool) error {
	values := url.Values{}

	values.Set("value", boolToStrInt(enable))

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v/1/set/feature/night",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.doer.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	ok, err := parseCommandResponse(req.Body)
	if !ok {
		return fmt.Errorf("failed to parse response %w", err)
	}

	return err
}

type commandResponse struct {
	Success bool   `json:"success,omitempty"`
	Error   string `json:"error,omitempty"`
}

func parseCommandResponse(r io.Reader) (bool, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return false, err
	}

	var resp commandResponse

	err = json.Unmarshal(data, &resp)
	if err != nil {
		return false, err
	}

	if !resp.Success {
		if resp.Error != "" {
			return true, fmt.Errorf("error from device %s", resp.Error)
		}

		return true, errors.New("unsuccessful request but no error defined")
	}

	if resp.Error != "" {
		return true, fmt.Errorf("error from device %s", resp.Error)
	}

	return true, nil
}

// SetFanSpeed sets the fan speed.
// This may return success but the unit may not actually change the speed
func (c *Client) SetFanSpeed(ctx context.Context, speed FanSpeed) error {
	values := url.Values{}

	values.Set("value", strconv.Itoa(int(speed)))

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v/1/set/fan",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.doer.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	ok, err := parseCommandResponse(req.Body)
	if !ok {
		return fmt.Errorf("failed to parse response %w", err)
	}

	return err
}

// SetMode sets the device mode.
// This may return success but the unit may not actually change the mode.
// My unit will return success for dehumidify but does not actuall support dehumidify.
func (c *Client) SetMode(ctx context.Context, mode DeviceMode) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v/1/set/mode/"+mode.String(),
		nil,
	)
	if err != nil {
		return err
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	ok, err := parseCommandResponse(req.Body)
	if !ok {
		return fmt.Errorf("failed to parse response %w", err)
	}

	return err
}
