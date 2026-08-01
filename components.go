package dgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unicode/utf8"
)

// ComponentType is type of component.
type ComponentType uint

// MessageComponent types.
const (
	ActionsRowComponent            ComponentType = 1
	ButtonComponent                ComponentType = 2
	SelectMenuComponent            ComponentType = 3
	TextInputComponent             ComponentType = 4
	UserSelectMenuComponent        ComponentType = 5
	RoleSelectMenuComponent        ComponentType = 6
	MentionableSelectMenuComponent ComponentType = 7
	ChannelSelectMenuComponent     ComponentType = 8
	SectionComponent               ComponentType = 9
	TextDisplayComponent           ComponentType = 10
	ThumbnailComponent             ComponentType = 11
	MediaGalleryComponent          ComponentType = 12
	FileComponentType              ComponentType = 13
	SeparatorComponent             ComponentType = 14
	ContainerComponent             ComponentType = 17
	LabelComponent                 ComponentType = 18
	FileUploadComponent            ComponentType = 19
	RadioGroupComponent            ComponentType = 21
	CheckboxGroupComponent         ComponentType = 22
	CheckboxComponent              ComponentType = 23
)

// MessageComponent is a base interface for all message components.
type MessageComponent interface {
	json.Marshaler
	Type() ComponentType
}

type unmarshalableMessageComponent struct {
	MessageComponent
}

// UnmarshalJSON is a helper function to unmarshal MessageComponent object.
func (umc *unmarshalableMessageComponent) UnmarshalJSON(src []byte) error {
	var v struct {
		Type ComponentType `json:"type"`
	}
	err := json.Unmarshal(src, &v)
	if err != nil {
		return err
	}

	switch v.Type {
	case ActionsRowComponent:
		umc.MessageComponent = &ActionsRow{}
	case ButtonComponent:
		umc.MessageComponent = &Button{}
	case SelectMenuComponent, ChannelSelectMenuComponent, UserSelectMenuComponent,
		RoleSelectMenuComponent, MentionableSelectMenuComponent:
		umc.MessageComponent = &SelectMenu{}
	case TextInputComponent:
		umc.MessageComponent = &TextInput{}
	case SectionComponent:
		umc.MessageComponent = &Section{}
	case TextDisplayComponent:
		umc.MessageComponent = &TextDisplay{}
	case ThumbnailComponent:
		umc.MessageComponent = &Thumbnail{}
	case MediaGalleryComponent:
		umc.MessageComponent = &MediaGallery{}
	case FileComponentType:
		umc.MessageComponent = &FileComponent{}
	case SeparatorComponent:
		umc.MessageComponent = &Separator{}
	case ContainerComponent:
		umc.MessageComponent = &Container{}
	case LabelComponent:
		umc.MessageComponent = &Label{}
	case FileUploadComponent:
		umc.MessageComponent = &FileUpload{}
	case RadioGroupComponent:
		umc.MessageComponent = &RadioGroup{}
	case CheckboxGroupComponent:
		umc.MessageComponent = &CheckboxGroup{}
	case CheckboxComponent:
		umc.MessageComponent = &Checkbox{}
	default:
		umc.MessageComponent = &UnknownComponent{}
	}
	return json.Unmarshal(src, umc.MessageComponent)
}

// MessageComponentFromJSON is a helper function for unmarshaling message components
func MessageComponentFromJSON(b []byte) (MessageComponent, error) {
	var u unmarshalableMessageComponent
	err := u.UnmarshalJSON(b)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal into MessageComponent: %w", err)
	}
	return u.MessageComponent, nil
}

// UnknownComponent preserves a component whose type is not yet known by this
// version of dgo. This allows interaction payloads to remain decodable when
// Discord introduces a new component type.
type UnknownComponent struct {
	ComponentType ComponentType   `json:"-"`
	Raw           json.RawMessage `json:"-"`
}

// Type returns the component type found in the original payload.
func (c UnknownComponent) Type() ComponentType {
	return c.ComponentType
}

// UnmarshalJSON preserves the complete unknown component payload.
func (c *UnknownComponent) UnmarshalJSON(data []byte) error {
	var header struct {
		Type ComponentType `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	c.ComponentType = header.Type
	c.Raw = append(c.Raw[:0], data...)
	return nil
}

// MarshalJSON writes the original unknown component payload unchanged.
func (c UnknownComponent) MarshalJSON() ([]byte, error) {
	if len(c.Raw) == 0 {
		return json.Marshal(struct {
			Type ComponentType `json:"type"`
		}{Type: c.ComponentType})
	}
	return append([]byte(nil), c.Raw...), nil
}

// ActionsRow is a top-level container component for displaying a row of interactive components.
type ActionsRow struct {
	// Can contain Button, SelectMenu and TextInput.
	// NOTE: maximum of 5.
	Components []MessageComponent `json:"components"`
	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`
}

// MarshalJSON is a method for marshaling ActionsRow to a JSON object.
func (r ActionsRow) MarshalJSON() ([]byte, error) {
	type actionsRow ActionsRow

	return Marshal(struct {
		actionsRow
		Type ComponentType `json:"type"`
	}{
		actionsRow: actionsRow(r),
		Type:       r.Type(),
	})
}

// UnmarshalJSON is a helper function to unmarshal Actions Row.
func (r *ActionsRow) UnmarshalJSON(data []byte) error {
	type actionsRow ActionsRow
	var v struct {
		actionsRow
		RawComponents []unmarshalableMessageComponent `json:"components"`
	}
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	*r = ActionsRow(v.actionsRow)

	r.Components = make([]MessageComponent, len(v.RawComponents))
	for i, v := range v.RawComponents {
		r.Components[i] = v.MessageComponent
	}

	return err
}

// Type is a method to get the type of a component.
func (r ActionsRow) Type() ComponentType {
	return ActionsRowComponent
}

// ButtonStyle is style of button.
type ButtonStyle uint

// Button styles.
const (
	// PrimaryButton is a button with blurple color.
	PrimaryButton ButtonStyle = 1
	// SecondaryButton is a button with grey color.
	SecondaryButton ButtonStyle = 2
	// SuccessButton is a button with green color.
	SuccessButton ButtonStyle = 3
	// DangerButton is a button with red color.
	DangerButton ButtonStyle = 4
	// LinkButton is a special type of button which navigates to a URL. Has grey color.
	LinkButton ButtonStyle = 5
	// PremiumButton is a special type of button with a blurple color that links to a SKU.
	PremiumButton ButtonStyle = 6
)

// ComponentEmoji represents button emoji, if it does have one.
type ComponentEmoji struct {
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
	Animated bool   `json:"animated,omitempty"`
}

// Button represents button component.
type Button struct {
	Label    string          `json:"label"`
	Style    ButtonStyle     `json:"style"`
	Disabled bool            `json:"disabled"`
	Emoji    *ComponentEmoji `json:"emoji,omitempty"`

	// NOTE: Only button with LinkButton style can have link. Also, URL is mutually exclusive with CustomID.
	URL      string `json:"url,omitempty"`
	CustomID string `json:"custom_id,omitempty"`
	// Identifier for a purchasable SKU. Only available when using premium-style buttons.
	SKUID string `json:"sku_id,omitempty"`
	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`
}

// MarshalJSON is a method for marshaling Button to a JSON object.
func (b Button) MarshalJSON() ([]byte, error) {
	type button Button

	if b.Style == 0 {
		b.Style = PrimaryButton
	}

	return Marshal(struct {
		button
		Type ComponentType `json:"type"`
	}{
		button: button(b),
		Type:   b.Type(),
	})
}

// Type is a method to get the type of a component.
func (Button) Type() ComponentType {
	return ButtonComponent
}

// SelectMenuOption represents an option for a select menu.
type SelectMenuOption struct {
	Label       string          `json:"label,omitempty"`
	Value       string          `json:"value"`
	Description string          `json:"description"`
	Emoji       *ComponentEmoji `json:"emoji,omitempty"`
	// Determines whenever option is selected by default or not.
	Default bool `json:"default"`
}

// SelectMenuDefaultValueType represents the type of an entity selected by default in auto-populated select menus.
type SelectMenuDefaultValueType string

// SelectMenuDefaultValue types.
const (
	SelectMenuDefaultValueUser    SelectMenuDefaultValueType = "user"
	SelectMenuDefaultValueRole    SelectMenuDefaultValueType = "role"
	SelectMenuDefaultValueChannel SelectMenuDefaultValueType = "channel"
)

// SelectMenuDefaultValue represents an entity selected by default in auto-populated select menus.
type SelectMenuDefaultValue struct {
	// ID of the entity.
	ID string `json:"id"`
	// Type of the entity.
	Type SelectMenuDefaultValueType `json:"type"`
}

// SelectMenuType represents select menu type.
type SelectMenuType ComponentType

// SelectMenu types.
const (
	StringSelectMenu      = SelectMenuType(SelectMenuComponent)
	UserSelectMenu        = SelectMenuType(UserSelectMenuComponent)
	RoleSelectMenu        = SelectMenuType(RoleSelectMenuComponent)
	MentionableSelectMenu = SelectMenuType(MentionableSelectMenuComponent)
	ChannelSelectMenu     = SelectMenuType(ChannelSelectMenuComponent)
)

// SelectMenu represents select menu component.
type SelectMenu struct {
	// Type of the select menu.
	MenuType SelectMenuType `json:"type,omitempty"`
	// CustomID is a developer-defined identifier for the select menu.
	CustomID string `json:"custom_id,omitempty"`
	// The text which will be shown in the menu if there's no default options or all options was deselected and component was closed.
	Placeholder string `json:"placeholder"`
	// This value determines the minimal amount of selected items in the menu.
	MinValues *int `json:"min_values,omitempty"`
	// This value determines the maximal amount of selected items in the menu.
	// If MaxValues or MinValues are greater than one then the user can select multiple items in the component.
	MaxValues int `json:"max_values,omitempty"`
	// List of default values for auto-populated select menus.
	// NOTE: Number of entries should be in the range defined by MinValues and MaxValues.
	DefaultValues []SelectMenuDefaultValue `json:"default_values,omitempty"`

	Options  []SelectMenuOption `json:"options,omitempty"`
	Disabled bool               `json:"disabled"`
	Required *bool              `json:"required,omitempty"`

	// NOTE: Can only be used in SelectMenu with Channel menu type.
	ChannelTypes []ChannelType `json:"channel_types,omitempty"`

	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`

	// Values is populated only when receiving a modal submit interaction.
	Values []string `json:"values,omitempty"`
}

// Type is a method to get the type of a component.
func (s SelectMenu) Type() ComponentType {
	if s.MenuType != 0 {
		return ComponentType(s.MenuType)
	}
	return SelectMenuComponent
}

// MarshalJSON is a method for marshaling SelectMenu to a JSON object.
func (s SelectMenu) MarshalJSON() ([]byte, error) {
	type selectMenu SelectMenu

	return Marshal(struct {
		selectMenu
		Type ComponentType `json:"type"`
	}{
		selectMenu: selectMenu(s),
		Type:       s.Type(),
	})
}

// TextInput represents text input component.
type TextInput struct {
	CustomID    string         `json:"custom_id"`
	Label       string         `json:"label"`
	Style       TextInputStyle `json:"style"`
	Placeholder string         `json:"placeholder,omitempty"`
	Value       string         `json:"value,omitempty"`
	Required    bool           `json:"required"`
	MinLength   int            `json:"min_length,omitempty"`
	MaxLength   int            `json:"max_length,omitempty"`

	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`
}

// Type is a method to get the type of a component.
func (TextInput) Type() ComponentType {
	return TextInputComponent
}

// MarshalJSON is a method for marshaling TextInput to a JSON object.
func (m TextInput) MarshalJSON() ([]byte, error) {
	type inputText TextInput

	return Marshal(struct {
		inputText
		Type ComponentType `json:"type"`
	}{
		inputText: inputText(m),
		Type:      m.Type(),
	})
}

// TextInputStyle is style of text in TextInput component.
type TextInputStyle uint

// Text styles
const (
	TextInputShort     TextInputStyle = 1
	TextInputParagraph TextInputStyle = 2
)

// Section is a top-level layout component that allows you to join text contextually with an accessory.
type Section struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`
	// Array of text display components; max of 3.
	Components []MessageComponent `json:"components"`
	// Can be Button or Thumbnail
	Accessory MessageComponent `json:"accessory"`
}

// UnmarshalJSON is a method for unmarshaling Section from JSON
func (s *Section) UnmarshalJSON(data []byte) error {
	type section Section

	var v struct {
		section
		RawComponents []unmarshalableMessageComponent `json:"components"`
		RawAccessory  unmarshalableMessageComponent   `json:"accessory"`
	}

	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}

	*s = Section(v.section)
	s.Accessory = v.RawAccessory.MessageComponent
	s.Components = make([]MessageComponent, len(v.RawComponents))
	for i, v := range v.RawComponents {
		s.Components[i] = v.MessageComponent
	}

	return nil
}

// Type is a method to get the type of a component.
func (Section) Type() ComponentType {
	return SectionComponent
}

// MarshalJSON is a method for marshaling Section to a JSON object.
func (s Section) MarshalJSON() ([]byte, error) {
	type section Section

	return Marshal(struct {
		section
		Type ComponentType `json:"type"`
	}{
		section: section(s),
		Type:    s.Type(),
	})
}

// TextDisplay is a top-level component that allows you to add markdown-formatted text to the message.
type TextDisplay struct {
	Content string `json:"content"`
}

// Type is a method to get the type of a component.
func (TextDisplay) Type() ComponentType {
	return TextDisplayComponent
}

// MarshalJSON is a method for marshaling TextDisplay to a JSON object.
func (t TextDisplay) MarshalJSON() ([]byte, error) {
	type textDisplay TextDisplay

	return Marshal(struct {
		textDisplay
		Type ComponentType `json:"type"`
	}{
		textDisplay: textDisplay(t),
		Type:        t.Type(),
	})
}

// Thumbnail component can be used as an accessory for a section component.
type Thumbnail struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID          int               `json:"id,omitempty"`
	Media       UnfurledMediaItem `json:"media"`
	Description *string           `json:"description,omitempty"`
	Spoiler     bool              `json:"spoiler,omitempty"`
}

// Type is a method to get the type of a component.
func (Thumbnail) Type() ComponentType {
	return ThumbnailComponent
}

// MarshalJSON is a method for marshaling Thumbnail to a JSON object.
func (t Thumbnail) MarshalJSON() ([]byte, error) {
	type thumbnail Thumbnail

	return Marshal(struct {
		thumbnail
		Type ComponentType `json:"type"`
	}{
		thumbnail: thumbnail(t),
		Type:      t.Type(),
	})
}

// MediaGallery is a top-level component allows you to group images, videos or gifs into a gallery grid.
type MediaGallery struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`
	// Array of media gallery items; max of 10.
	Items []MediaGalleryItem `json:"items"`
}

// Type is a method to get the type of a component.
func (MediaGallery) Type() ComponentType {
	return MediaGalleryComponent
}

// MarshalJSON is a method for marshaling MediaGallery to a JSON object.
func (m MediaGallery) MarshalJSON() ([]byte, error) {
	type mediaGallery MediaGallery

	return Marshal(struct {
		mediaGallery
		Type ComponentType `json:"type"`
	}{
		mediaGallery: mediaGallery(m),
		Type:         m.Type(),
	})
}

// MediaGalleryItem represents an item used in MediaGallery.
type MediaGalleryItem struct {
	Media       UnfurledMediaItem `json:"media"`
	Description *string           `json:"description,omitempty"`
	Spoiler     bool              `json:"spoiler"`
}

// FileComponent is a top-level component that allows you to display an uploaded file as an attachment to the message and reference it in the component.
type FileComponent struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID      int               `json:"id,omitempty"`
	File    UnfurledMediaItem `json:"file"`
	Spoiler bool              `json:"spoiler"`
}

// Type is a method to get the type of a component.
func (FileComponent) Type() ComponentType {
	return FileComponentType
}

// MarshalJSON is a method for marshaling FileComponent to a JSON object.
func (f FileComponent) MarshalJSON() ([]byte, error) {
	type fileComponent FileComponent

	return Marshal(struct {
		fileComponent
		Type ComponentType `json:"type"`
	}{
		fileComponent: fileComponent(f),
		Type:          f.Type(),
	})
}

// SeparatorSpacingSize represents spacing size around the separator.
type SeparatorSpacingSize uint

// Separator spacing sizes.
const (
	SeparatorSpacingSizeSmall SeparatorSpacingSize = 1
	SeparatorSpacingSizeLarge SeparatorSpacingSize = 2
)

// Separator is a top-level layout component that adds vertical padding and visual division between other components.
type Separator struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID int `json:"id,omitempty"`

	Divider *bool                 `json:"divider,omitempty"`
	Spacing *SeparatorSpacingSize `json:"spacing,omitempty"`
}

// Type is a method to get the type of a component.
func (Separator) Type() ComponentType {
	return SeparatorComponent
}

// MarshalJSON is a method for marshaling Separator to a JSON object.
func (s Separator) MarshalJSON() ([]byte, error) {
	type separator Separator

	return Marshal(struct {
		separator
		Type ComponentType `json:"type"`
	}{
		separator: separator(s),
		Type:      s.Type(),
	})
}

// Container is a top-level layout component.
// Containers are visually distinct from surrounding components and have an optional customizable color bar (similar to embeds).
type Container struct {
	// Unique identifier for the component; auto populated through increment if not provided.
	ID          int                `json:"id,omitempty"`
	AccentColor *int               `json:"accent_color,omitempty"`
	Spoiler     bool               `json:"spoiler"`
	Components  []MessageComponent `json:"components"`
}

// Type is a method to get the type of a component.
func (Container) Type() ComponentType {
	return ContainerComponent
}

// UnmarshalJSON is a method for unmarshaling Container from JSON
func (c *Container) UnmarshalJSON(data []byte) error {
	type container Container

	var v struct {
		container
		RawComponents []unmarshalableMessageComponent `json:"components"`
	}

	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}

	*c = Container(v.container)
	c.Components = make([]MessageComponent, len(v.RawComponents))
	for i, v := range v.RawComponents {
		c.Components[i] = v.MessageComponent
	}

	return nil
}

// MarshalJSON is a method for marshaling Container to a JSON object.
func (c Container) MarshalJSON() ([]byte, error) {
	type container Container

	return Marshal(struct {
		container
		Type ComponentType `json:"type"`
	}{
		container: container(c),
		Type:      c.Type(),
	})
}

// Label is a top-level modal layout component that associates text with one
// interactive child component.
type Label struct {
	ID          int              `json:"id,omitempty"`
	Label       string           `json:"label"`
	Description string           `json:"description,omitempty"`
	Component   MessageComponent `json:"component"`
}

// NewLabel creates a Label containing a modal component.
func NewLabel(label string, component MessageComponent) *Label {
	return &Label{Label: label, Component: component}
}

// Type returns the component type.
func (Label) Type() ComponentType {
	return LabelComponent
}

// UnmarshalJSON decodes the nested label component.
func (l *Label) UnmarshalJSON(data []byte) error {
	type label Label
	var v struct {
		label
		RawComponent unmarshalableMessageComponent `json:"component"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*l = Label(v.label)
	l.Component = v.RawComponent.MessageComponent
	return nil
}

// MarshalJSON is a method for marshaling Label to a JSON object.
func (l Label) MarshalJSON() ([]byte, error) {
	type label Label
	return Marshal(struct {
		label
		Type ComponentType `json:"type"`
	}{
		label: label(l),
		Type:  l.Type(),
	})
}

// FileUpload is an interactive modal component for uploading files.
type FileUpload struct {
	ID        int      `json:"id,omitempty"`
	CustomID  string   `json:"custom_id,omitempty"`
	MinValues *int     `json:"min_values,omitempty"`
	MaxValues int      `json:"max_values,omitempty"`
	Required  *bool    `json:"required,omitempty"`
	Values    []string `json:"values,omitempty"`
}

// NewFileUpload creates a FileUpload with the given custom ID.
func NewFileUpload(customID string) *FileUpload {
	return &FileUpload{CustomID: customID}
}

// Type returns the component type.
func (FileUpload) Type() ComponentType {
	return FileUploadComponent
}

// MarshalJSON is a method for marshaling FileUpload to a JSON object.
func (f FileUpload) MarshalJSON() ([]byte, error) {
	type fileUpload FileUpload
	return Marshal(struct {
		fileUpload
		Type ComponentType `json:"type"`
	}{
		fileUpload: fileUpload(f),
		Type:       f.Type(),
	})
}

// ModalChoiceOption is an option in a RadioGroup or CheckboxGroup.
type ModalChoiceOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// RadioGroup is an interactive modal component for choosing exactly one option.
type RadioGroup struct {
	ID       int                 `json:"id,omitempty"`
	CustomID string              `json:"custom_id,omitempty"`
	Options  []ModalChoiceOption `json:"options,omitempty"`
	Required *bool               `json:"required,omitempty"`
	Value    *string             `json:"value,omitempty"`
}

// NewRadioGroup creates a RadioGroup with the given custom ID and options.
func NewRadioGroup(customID string, options ...ModalChoiceOption) *RadioGroup {
	return &RadioGroup{CustomID: customID, Options: options}
}

// Type returns the component type.
func (RadioGroup) Type() ComponentType {
	return RadioGroupComponent
}

// MarshalJSON is a method for marshaling RadioGroup to a JSON object.
func (r RadioGroup) MarshalJSON() ([]byte, error) {
	type radioGroup RadioGroup
	return Marshal(struct {
		radioGroup
		Type ComponentType `json:"type"`
	}{
		radioGroup: radioGroup(r),
		Type:       r.Type(),
	})
}

// CheckboxGroup is an interactive modal component for choosing one or more options.
type CheckboxGroup struct {
	ID        int                 `json:"id,omitempty"`
	CustomID  string              `json:"custom_id,omitempty"`
	Options   []ModalChoiceOption `json:"options,omitempty"`
	MinValues *int                `json:"min_values,omitempty"`
	MaxValues int                 `json:"max_values,omitempty"`
	Required  *bool               `json:"required,omitempty"`
	Values    []string            `json:"values,omitempty"`
}

// NewCheckboxGroup creates a CheckboxGroup with the given custom ID and options.
func NewCheckboxGroup(customID string, options ...ModalChoiceOption) *CheckboxGroup {
	return &CheckboxGroup{CustomID: customID, Options: options}
}

// Type returns the component type.
func (CheckboxGroup) Type() ComponentType {
	return CheckboxGroupComponent
}

// MarshalJSON is a method for marshaling CheckboxGroup to a JSON object.
func (c CheckboxGroup) MarshalJSON() ([]byte, error) {
	type checkboxGroup CheckboxGroup
	return Marshal(struct {
		checkboxGroup
		Type ComponentType `json:"type"`
	}{
		checkboxGroup: checkboxGroup(c),
		Type:          c.Type(),
	})
}

// Checkbox is an interactive modal component for a single boolean choice.
type Checkbox struct {
	ID       int    `json:"id,omitempty"`
	CustomID string `json:"custom_id,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Value    bool   `json:"value,omitempty"`
}

// NewCheckbox creates a Checkbox with the given custom ID.
func NewCheckbox(customID string) *Checkbox {
	return &Checkbox{CustomID: customID}
}

// Type returns the component type.
func (Checkbox) Type() ComponentType {
	return CheckboxComponent
}

// MarshalJSON is a method for marshaling Checkbox to a JSON object.
func (c Checkbox) MarshalJSON() ([]byte, error) {
	type checkbox Checkbox
	return Marshal(struct {
		checkbox
		Type ComponentType `json:"type"`
	}{
		checkbox: checkbox(c),
		Type:     c.Type(),
	})
}

// UnfurledMediaItem represents an unfurled media item.
type UnfurledMediaItem struct {
	URL string `json:"url"`
}

// UnfurledMediaItemLoadingState is the loading state of the unfurled media item.
type UnfurledMediaItemLoadingState uint

// Unfurled media item loading states.
const (
	UnfurledMediaItemLoadingStateUnknown        UnfurledMediaItemLoadingState = 0
	UnfurledMediaItemLoadingStateLoading        UnfurledMediaItemLoadingState = 1
	UnfurledMediaItemLoadingStateLoadingSuccess UnfurledMediaItemLoadingState = 2
	UnfurledMediaItemLoadingStateLoadedNotFound UnfurledMediaItemLoadingState = 3
)

// ResolvedUnfurledMediaItem represents a resolved unfurled media item.
type ResolvedUnfurledMediaItem struct {
	URL         string `json:"url"`
	ProxyURL    string `json:"proxy_url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
}

// ErrInvalidModal indicates that a modal or one of its components violates
// Discord's component constraints.
var ErrInvalidModal = errors.New("invalid modal")

// ValidateModal validates a modal response before it is sent to Discord.
func ValidateModal(customID, title string, components []MessageComponent) error {
	if err := validateComponentString("custom_id", customID, 1, 100); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModal, err)
	}
	if err := validateComponentString("title", title, 1, 45); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModal, err)
	}
	if len(components) < 1 || len(components) > 5 {
		return fmt.Errorf("%w: components must contain between 1 and 5 items", ErrInvalidModal)
	}

	customIDs := make(map[string]struct{})
	for i, component := range components {
		if isNilMessageComponent(component) {
			return fmt.Errorf("%w: components[%d] must not be nil", ErrInvalidModal, i)
		}
		switch component.Type() {
		case LabelComponent, ActionsRowComponent, TextDisplayComponent:
		default:
			return fmt.Errorf("%w: component type %d cannot be used at the top level", ErrInvalidModal, component.Type())
		}
		if err := validateModalComponent(component, customIDs); err != nil {
			return fmt.Errorf("%w: components[%d]: %v", ErrInvalidModal, i, err)
		}
	}
	return nil
}

func validateModalComponent(component MessageComponent, customIDs map[string]struct{}) error {
	switch c := component.(type) {
	case Label:
		return validateLabel(c, customIDs)
	case *Label:
		if c == nil {
			return errors.New("label must not be nil")
		}
		return validateLabel(*c, customIDs)
	case ActionsRow:
		return validateModalActionsRow(c, customIDs)
	case *ActionsRow:
		if c == nil {
			return errors.New("action row must not be nil")
		}
		return validateModalActionsRow(*c, customIDs)
	case TextDisplay:
		return validateComponentString("content", c.Content, 1, 4000)
	case *TextDisplay:
		if c == nil {
			return errors.New("text display must not be nil")
		}
		return validateComponentString("content", c.Content, 1, 4000)
	case TextInput:
		return validateTextInput(c, customIDs)
	case *TextInput:
		if c == nil {
			return errors.New("text input must not be nil")
		}
		return validateTextInput(*c, customIDs)
	case SelectMenu:
		return validateModalSelect(c, customIDs)
	case *SelectMenu:
		if c == nil {
			return errors.New("select menu must not be nil")
		}
		return validateModalSelect(*c, customIDs)
	case FileUpload:
		return validateFileUpload(c, customIDs)
	case *FileUpload:
		if c == nil {
			return errors.New("file upload must not be nil")
		}
		return validateFileUpload(*c, customIDs)
	case RadioGroup:
		return validateRadioGroup(c, customIDs)
	case *RadioGroup:
		if c == nil {
			return errors.New("radio group must not be nil")
		}
		return validateRadioGroup(*c, customIDs)
	case CheckboxGroup:
		return validateCheckboxGroup(c, customIDs)
	case *CheckboxGroup:
		if c == nil {
			return errors.New("checkbox group must not be nil")
		}
		return validateCheckboxGroup(*c, customIDs)
	case Checkbox:
		return validateCustomID(c.CustomID, customIDs)
	case *Checkbox:
		if c == nil {
			return errors.New("checkbox must not be nil")
		}
		return validateCustomID(c.CustomID, customIDs)
	default:
		return fmt.Errorf("unsupported component implementation %T", component)
	}
}

func validateLabel(label Label, customIDs map[string]struct{}) error {
	if err := validateComponentString("label", label.Label, 1, 45); err != nil {
		return err
	}
	if err := validateComponentString("description", label.Description, 0, 100); err != nil {
		return err
	}
	if isNilMessageComponent(label.Component) {
		return errors.New("component must not be nil")
	}
	switch label.Component.Type() {
	case SelectMenuComponent, TextInputComponent, UserSelectMenuComponent,
		RoleSelectMenuComponent, MentionableSelectMenuComponent, ChannelSelectMenuComponent,
		FileUploadComponent, RadioGroupComponent, CheckboxGroupComponent, CheckboxComponent:
	default:
		return fmt.Errorf("component type %d cannot be used in a label", label.Component.Type())
	}
	return validateModalComponent(label.Component, customIDs)
}

func validateModalActionsRow(row ActionsRow, customIDs map[string]struct{}) error {
	if len(row.Components) != 1 {
		return errors.New("a modal action row must contain exactly one component")
	}
	component := row.Components[0]
	if isNilMessageComponent(component) {
		return errors.New("action row component must not be nil")
	}
	switch component.Type() {
	case TextInputComponent, SelectMenuComponent, UserSelectMenuComponent,
		RoleSelectMenuComponent, MentionableSelectMenuComponent, ChannelSelectMenuComponent:
	default:
		return fmt.Errorf("component type %d cannot be used in a modal action row", component.Type())
	}
	return validateModalComponent(component, customIDs)
}

func validateTextInput(input TextInput, customIDs map[string]struct{}) error {
	if err := validateCustomID(input.CustomID, customIDs); err != nil {
		return err
	}
	if input.Style != TextInputShort && input.Style != TextInputParagraph {
		return errors.New("style must be TextInputShort or TextInputParagraph")
	}
	if input.MinLength < 0 || input.MinLength > 4000 {
		return errors.New("min_length must be between 0 and 4000")
	}
	if input.MaxLength < 0 || input.MaxLength > 4000 {
		return errors.New("max_length must be between 1 and 4000 when provided")
	}
	if input.MaxLength > 0 && input.MinLength > input.MaxLength {
		return errors.New("min_length must not exceed max_length")
	}
	if err := validateComponentString("placeholder", input.Placeholder, 0, 100); err != nil {
		return err
	}
	if err := validateComponentString("value", input.Value, 0, 4000); err != nil {
		return err
	}
	return nil
}

func validateModalSelect(menu SelectMenu, customIDs map[string]struct{}) error {
	if err := validateCustomID(menu.CustomID, customIDs); err != nil {
		return err
	}
	maxValues := menu.MaxValues
	if maxValues == 0 {
		maxValues = 1
	}
	limit := 25
	if menu.Type() != SelectMenuComponent && len(menu.Options) != 0 {
		return errors.New("options are only valid for string select menus")
	}
	if menu.Type() == SelectMenuComponent {
		if len(menu.Options) < 1 || len(menu.Options) > limit {
			return errors.New("options must contain between 1 and 25 items")
		}
		for i, option := range menu.Options {
			if err := validateComponentString("option label", option.Label, 1, 100); err != nil {
				return fmt.Errorf("options[%d]: %v", i, err)
			}
			if err := validateComponentString("option value", option.Value, 1, 100); err != nil {
				return fmt.Errorf("options[%d]: %v", i, err)
			}
			if err := validateComponentString("option description", option.Description, 0, 100); err != nil {
				return fmt.Errorf("options[%d]: %v", i, err)
			}
		}
	}
	if err := validateMinMax(menu.MinValues, maxValues, limit, menu.Required); err != nil {
		return err
	}
	if err := validateComponentString("placeholder", menu.Placeholder, 0, 150); err != nil {
		return err
	}
	return nil
}

func validateFileUpload(upload FileUpload, customIDs map[string]struct{}) error {
	if err := validateCustomID(upload.CustomID, customIDs); err != nil {
		return err
	}
	maxValues := upload.MaxValues
	if maxValues == 0 {
		maxValues = 1
	}
	return validateMinMax(upload.MinValues, maxValues, 10, upload.Required)
}

func validateRadioGroup(group RadioGroup, customIDs map[string]struct{}) error {
	if err := validateCustomID(group.CustomID, customIDs); err != nil {
		return err
	}
	if len(group.Options) < 2 || len(group.Options) > 10 {
		return errors.New("options must contain between 2 and 10 items")
	}
	defaults, err := validateModalChoiceOptions(group.Options)
	if err != nil {
		return err
	}
	if defaults > 1 {
		return errors.New("only one radio option may be selected by default")
	}
	return nil
}

func validateCheckboxGroup(group CheckboxGroup, customIDs map[string]struct{}) error {
	if err := validateCustomID(group.CustomID, customIDs); err != nil {
		return err
	}
	if len(group.Options) < 1 || len(group.Options) > 10 {
		return errors.New("options must contain between 1 and 10 items")
	}
	defaults, err := validateModalChoiceOptions(group.Options)
	if err != nil {
		return err
	}
	maxValues := group.MaxValues
	if maxValues == 0 {
		maxValues = len(group.Options)
	}
	if err := validateMinMax(group.MinValues, maxValues, len(group.Options), group.Required); err != nil {
		return err
	}
	if defaults > maxValues {
		return errors.New("the number of default options must not exceed max_values")
	}
	return nil
}

func validateModalChoiceOptions(options []ModalChoiceOption) (int, error) {
	values := make(map[string]struct{}, len(options))
	defaults := 0
	for i, option := range options {
		if err := validateComponentString("option label", option.Label, 1, 100); err != nil {
			return 0, fmt.Errorf("options[%d]: %v", i, err)
		}
		if err := validateComponentString("option value", option.Value, 1, 100); err != nil {
			return 0, fmt.Errorf("options[%d]: %v", i, err)
		}
		if err := validateComponentString("option description", option.Description, 0, 100); err != nil {
			return 0, fmt.Errorf("options[%d]: %v", i, err)
		}
		if _, exists := values[option.Value]; exists {
			return 0, fmt.Errorf("options[%d]: value %q is duplicated", i, option.Value)
		}
		values[option.Value] = struct{}{}
		if option.Default {
			defaults++
		}
	}
	return defaults, nil
}

func validateMinMax(minValues *int, maxValues, limit int, required *bool) error {
	minimum := 1
	if minValues != nil {
		minimum = *minValues
	}
	if minimum < 0 || minimum > limit {
		return fmt.Errorf("min_values must be between 0 and %d", limit)
	}
	if maxValues < 1 || maxValues > limit {
		return fmt.Errorf("max_values must be between 1 and %d", limit)
	}
	if minimum > maxValues {
		return errors.New("min_values must not exceed max_values")
	}
	if minimum == 0 && (required == nil || *required) {
		return errors.New("min_values may be 0 only when required is false")
	}
	return nil
}

func validateCustomID(customID string, customIDs map[string]struct{}) error {
	if err := validateComponentString("custom_id", customID, 1, 100); err != nil {
		return err
	}
	if _, exists := customIDs[customID]; exists {
		return fmt.Errorf("custom_id %q is duplicated", customID)
	}
	customIDs[customID] = struct{}{}
	return nil
}

func validateComponentString(field, value string, minLength, maxLength int) error {
	length := utf8.RuneCountInString(value)
	if length < minLength || length > maxLength {
		if minLength == 0 {
			return fmt.Errorf("%s must be at most %d characters", field, maxLength)
		}
		return fmt.Errorf("%s must be between %d and %d characters", field, minLength, maxLength)
	}
	return nil
}

func isNilMessageComponent(component MessageComponent) bool {
	if component == nil {
		return true
	}
	value := reflect.ValueOf(component)
	return value.Kind() == reflect.Ptr && value.IsNil()
}
