package dgo

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestCurrentModalComponentsDecode(t *testing.T) {
	const payload = `{
		"custom_id":"survey",
		"components":[
			{"type":18,"id":1,"component":{"type":19,"id":2,"custom_id":"files","values":["100"]}},
			{"type":18,"id":3,"component":{"type":21,"id":4,"custom_id":"class","value":"wizard"}},
			{"type":18,"id":5,"component":{"type":22,"id":6,"custom_id":"days","values":["monday","friday"]}},
			{"type":18,"id":7,"component":{"type":23,"id":8,"custom_id":"agree","value":true}}
		],
		"resolved":{"attachments":{"100":{"id":"100","filename":"proof.png","content_type":"image/png"}}}
	}`

	var data ModalSubmitInteractionData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("unmarshal modal submit: %v", err)
	}
	if data.CustomID != "survey" || len(data.Components) != 4 {
		t.Fatalf("unexpected modal data: %#v", data)
	}

	fileLabel, ok := data.Components[0].(*Label)
	if !ok {
		t.Fatalf("first component is %T, want *Label", data.Components[0])
	}
	fileUpload, ok := fileLabel.Component.(*FileUpload)
	if !ok {
		t.Fatalf("label component is %T, want *FileUpload", fileLabel.Component)
	}
	if !reflect.DeepEqual(fileUpload.Values, []string{"100"}) {
		t.Fatalf("file values = %#v", fileUpload.Values)
	}
	if attachment := data.Resolved.Attachments["100"]; attachment == nil || attachment.Filename != "proof.png" {
		t.Fatalf("resolved attachment = %#v", attachment)
	}

	radio := data.Components[1].(*Label).Component.(*RadioGroup)
	if radio.Value == nil || *radio.Value != "wizard" {
		t.Fatalf("radio value = %#v", radio.Value)
	}

	group := data.Components[2].(*Label).Component.(*CheckboxGroup)
	if !reflect.DeepEqual(group.Values, []string{"monday", "friday"}) {
		t.Fatalf("checkbox group values = %#v", group.Values)
	}

	checkbox := data.Components[3].(*Label).Component.(*Checkbox)
	if !checkbox.Value {
		t.Fatal("checkbox value was not decoded")
	}
}

func TestCurrentModalComponentsMarshal(t *testing.T) {
	required := false
	components := []MessageComponent{
		NewLabel("Upload", &FileUpload{CustomID: "files", Required: &required}),
		NewRadioGroup("class",
			ModalChoiceOption{Label: "Wizard", Value: "wizard"},
			ModalChoiceOption{Label: "Warrior", Value: "warrior"},
		),
		NewCheckboxGroup("days", ModalChoiceOption{Label: "Monday", Value: "monday"}),
		NewCheckbox("agree"),
	}

	wantTypes := []ComponentType{
		LabelComponent,
		RadioGroupComponent,
		CheckboxGroupComponent,
		CheckboxComponent,
	}
	for i, component := range components {
		t.Run(reflect.TypeOf(component).String(), func(t *testing.T) {
			data, err := json.Marshal(component)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var header struct {
				Type ComponentType `json:"type"`
			}
			if err := json.Unmarshal(data, &header); err != nil {
				t.Fatalf("unmarshal header: %v", err)
			}
			if header.Type != wantTypes[i] {
				t.Fatalf("type = %d, want %d; JSON: %s", header.Type, wantTypes[i], data)
			}
		})
	}
}

func TestUnknownComponentPreservesPayload(t *testing.T) {
	const payload = `{"type":99,"future":"value","nested":{"enabled":true}}`

	component, err := MessageComponentFromJSON([]byte(payload))
	if err != nil {
		t.Fatalf("decode unknown component: %v", err)
	}
	unknown, ok := component.(*UnknownComponent)
	if !ok {
		t.Fatalf("component is %T, want *UnknownComponent", component)
	}
	if unknown.Type() != 99 {
		t.Fatalf("type = %d, want 99", unknown.Type())
	}

	got, err := json.Marshal(unknown)
	if err != nil {
		t.Fatalf("marshal unknown component: %v", err)
	}
	var gotObject, wantObject map[string]any
	if err := json.Unmarshal(got, &gotObject); err != nil {
		t.Fatalf("unmarshal marshaled component: %v", err)
	}
	if err := json.Unmarshal([]byte(payload), &wantObject); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if !reflect.DeepEqual(gotObject, wantObject) {
		t.Fatalf("unknown component changed:\n got: %s\nwant: %s", got, payload)
	}
}

func TestValidateCurrentModalComponents(t *testing.T) {
	optional := false
	minimum := 0
	components := []MessageComponent{
		NewLabel("Upload proof", &FileUpload{
			CustomID:  "files",
			MinValues: &minimum,
			Required:  &optional,
		}),
		NewLabel("Choose a class", NewRadioGroup("class",
			ModalChoiceOption{Label: "Wizard", Value: "wizard"},
			ModalChoiceOption{Label: "Warrior", Value: "warrior"},
		)),
		NewLabel("Choose days", NewCheckboxGroup("days",
			ModalChoiceOption{Label: "Monday", Value: "monday"},
		)),
		NewLabel("Agree?", NewCheckbox("agree")),
	}

	if err := ValidateModal("survey", "Survey", components); err != nil {
		t.Fatalf("valid modal was rejected: %v", err)
	}
}

func TestValidateModalRejectsInvalidComponents(t *testing.T) {
	zero := 0
	tooManyDefaults := []ModalChoiceOption{
		{Label: "One", Value: "one", Default: true},
		{Label: "Two", Value: "two", Default: true},
	}
	tests := []struct {
		name       string
		components []MessageComponent
	}{
		{
			name: "duplicate custom ID",
			components: []MessageComponent{
				NewLabel("First", NewCheckbox("duplicate")),
				NewLabel("Second", NewCheckbox("duplicate")),
			},
		},
		{
			name: "invalid label child",
			components: []MessageComponent{
				NewLabel("Bad", &Button{CustomID: "button"}),
			},
		},
		{
			name:       "typed nil component",
			components: []MessageComponent{(*Label)(nil)},
		},
		{
			name: "too few radio options",
			components: []MessageComponent{
				NewLabel("Class", NewRadioGroup("class", ModalChoiceOption{Label: "One", Value: "one"})),
			},
		},
		{
			name: "required file upload with zero minimum",
			components: []MessageComponent{
				NewLabel("Files", &FileUpload{CustomID: "files", MinValues: &zero}),
			},
		},
		{
			name: "too many checkbox defaults",
			components: []MessageComponent{
				NewLabel("Choices", &CheckboxGroup{
					CustomID:  "choices",
					Options:   tooManyDefaults,
					MaxValues: 1,
				}),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateModal("survey", "Survey", test.components)
			if !errors.Is(err, ErrInvalidModal) {
				t.Fatalf("error = %v, want ErrInvalidModal", err)
			}
		})
	}
}

func TestInteractionRespondValidatesModalBeforeRequest(t *testing.T) {
	err := (&Session{}).InteractionRespond(
		&Interaction{ID: "1", Token: "token"},
		&InteractionResponse{
			Type: InteractionResponseModal,
			Data: &InteractionResponseData{
				CustomID:   "survey",
				Title:      "",
				Components: []MessageComponent{NewLabel("Agree?", NewCheckbox("agree"))},
			},
		},
	)
	if !errors.Is(err, ErrInvalidModal) {
		t.Fatalf("error = %v, want ErrInvalidModal", err)
	}
}
