package sidecar

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSuccessResponse_MarshalsResultAlwaysPresent(t *testing.T) {
	got, err := json.Marshal(SuccessResponse("req-1", map[string]any{"ok": true}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"id":"req-1","result":{"ok":true}}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestSuccessResponse_NilResult_MarshalsResultKeyAsNull(t *testing.T) {
	got, err := json.Marshal(SuccessResponse("req-2", nil))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"id":"req-2","result":null}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestErrorResponse_MarshalsErrorObjectNoResultKey(t *testing.T) {
	got, err := json.Marshal(ErrorResponse("req-3", ErrNoDeviceSelected, "No device selected", nil))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"id":"req-3","error":{"code":"NoDeviceSelected","message":"No device selected"}}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestErrorResponse_WithData_IncludesDataKey(t *testing.T) {
	got, err := json.Marshal(ErrorResponse("req-4", ErrSelectorNotFound, "not found", map[string]any{"selector": "id=foo"}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"id":"req-4","error":{"code":"SelectorNotFound","message":"not found","data":{"selector":"id=foo"}}}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestEvent_MarshalsEventAndPayload(t *testing.T) {
	got, err := json.Marshal(Event{Event: "device.connected", Payload: map[string]any{"id": "emulator-5554"}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"event":"device.connected","payload":{"id":"emulator-5554"}}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestParseRequest_ValidRequest_DecodesIDMethodParams(t *testing.T) {
	req, err := ParseRequest([]byte(`{"id":"1","method":"device.list","params":{"filter":"online"}}`))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if req.ID != "1" || req.Method != "device.list" {
		t.Fatalf("ParseRequest() = %+v, want id=1 method=device.list", req)
	}
	if string(req.Params) != `{"filter":"online"}` {
		t.Fatalf("ParseRequest() Params = %s, want the raw params object", req.Params)
	}
}

func TestParseRequest_NoParams_LeavesParamsNil(t *testing.T) {
	req, err := ParseRequest([]byte(`{"id":"1","method":"device.list"}`))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if req.Params != nil {
		t.Fatalf("ParseRequest() Params = %s, want nil", req.Params)
	}
}

func TestParseRequest_MalformedJSON_ReturnsMalformedJSONError(t *testing.T) {
	_, err := ParseRequest([]byte(`{not json`))
	if !errors.Is(err, errMalformedJSON) {
		t.Fatalf("ParseRequest() error = %v, want errMalformedJSON", err)
	}
}

func TestParseRequest_MissingMethod_ReturnsMissingFieldsError(t *testing.T) {
	_, err := ParseRequest([]byte(`{"id":"1"}`))
	if !errors.Is(err, errMissingFields) {
		t.Fatalf("ParseRequest() error = %v, want errMissingFields", err)
	}
}

func TestParseRequest_MissingID_ReturnsMissingFieldsError(t *testing.T) {
	_, err := ParseRequest([]byte(`{"method":"device.list"}`))
	if !errors.Is(err, errMissingFields) {
		t.Fatalf("ParseRequest() error = %v, want errMissingFields", err)
	}
}

func TestParseRequest_NonStringID_ReturnsMissingFieldsError(t *testing.T) {
	_, err := ParseRequest([]byte(`{"id":1,"method":"device.list"}`))
	if !errors.Is(err, errMissingFields) {
		t.Fatalf("ParseRequest() error = %v, want errMissingFields", err)
	}
}
