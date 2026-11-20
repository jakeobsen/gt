package kit

import "time"

// View is a screen a command shows. *List is the only kind so far.
type View interface{ isView() }

// List is a searchable list of items in sections, like Raycast's List. Its
// contents come from Load, which runs off the UI loop: when the list opens,
// and again whenever a command calls Context.Refresh.
type List struct {
	// Title is shown in the header after "goto", like Raycast's
	// navigationTitle. It defaults to the title of the command that opened it.
	Title string

	// SearchPlaceholder is shown in the empty search box.
	SearchPlaceholder string

	// Load returns the list's contents. While it runs, goto shows that the
	// list is loading; on a refresh the previous contents stay visible.
	Load func(ctx Context) (Content, error)

	// Filter, when set, adds a dropdown that narrows the list, like Raycast's
	// List.Dropdown. Tab opens it; Load reads the choice with Context.Filter
	// and runs again whenever it changes.
	Filter *Filter

	// ShowDetail splits the screen: the list on the left and the selected
	// item's Detail on the right, like Raycast's isShowingDetail.
	ShowDetail bool
}

func (*List) isView() {}

// Filter is a list's dropdown of choices.
type Filter struct {
	// Title names what is being filtered, e.g. "Tag".
	Title   string
	Options []FilterOption
	// Default is the Value chosen when the list opens.
	Default string
}

// FilterOption is one choice in a Filter.
type FilterOption struct {
	Title string
	Value string
}

// Content is everything a List shows.
type Content struct {
	Sections []Section
	// Empty is shown when no section has any items.
	Empty Empty
	// RefreshAfter, when positive, loads the list again after this long,
	// quietly (no loading indicator) — for progress or countdowns. It is
	// cancelled when the screen closes or a newer load replaces this one.
	RefreshAfter time.Duration
}

// Section groups items under an optional title. Sections without items are
// not shown.
type Section struct {
	Title string
	// Subtitle is shown dimmed after the title, e.g. a count.
	Subtitle string
	Items    []Item
}

// Item is one row of a list.
type Item struct {
	Title    string
	Subtitle string
	// Keywords are extra words the search matches against.
	Keywords []string
	// Accessories are short tags shown at the right edge of the row.
	Accessories []Accessory
	// Actions are what the user can do with the item. The first one that
	// isn't a Divider is the primary action, run by Enter; ctrl+k lists all.
	Actions []Action
	// Detail is shown beside the list when the List has ShowDetail, like
	// Raycast's List.Item.Detail. It is plain text with a little Markdown:
	// "## " headings, **bold** and ``` code blocks.
	Detail string
}

// Accessory is a short tag at the right edge of a row.
type Accessory struct {
	Text  string
	Color Color
}

// Color is a semantic colour for accessories.
type Color int

const (
	// Dim is the default: secondary text.
	Dim Color = iota
	Red
	Orange
	Green
)

// Empty describes what a list shows when it has no items.
type Empty struct {
	Title       string
	Description string
	// Actions work like an item's actions while the list is empty.
	Actions []Action
}

// Action is something the user can run on an item, from the ctrl+k menu or
// with its shortcut.
type Action struct {
	Title string
	// Shortcut is a key like "ctrl+g", as Bubble Tea names keys. Terminals
	// can't send ⌘, so shortcuts use ctrl. Avoid keys goto uses itself:
	// enter, esc, ctrl+c, ctrl+k, the arrows, pgup and pgdown.
	Shortcut string
	// Destructive actions are shown in red.
	Destructive bool
	// Run performs the action off the UI loop. A returned error is shown as
	// a failure toast.
	Run func(ctx Context) error

	divider bool
}

// Divider separates groups of actions in the ctrl+k menu, like an
// ActionPanel.Section boundary in Raycast.
var Divider = Action{divider: true}

// IsDivider reports whether a is the Divider.
func (a Action) IsDivider() bool { return a.divider }

// ToastStyle is the kind of message a toast shows.
type ToastStyle int

const (
	// Loading is for work in progress.
	Loading ToastStyle = iota
	Success
	Failure
)

// Toast is a message in goto's status line that a command can update, like
// Raycast's Toast.
type Toast interface {
	Update(style ToastStyle, title, message string)
}

// Confirmation describes a yes/no question, like Raycast's confirmAlert.
type Confirmation struct {
	Title   string
	Message string
	// Action is the label of the confirming choice, e.g. "Move to Trash".
	Action string
	// Destructive shows the confirming choice in red.
	Destructive bool
}

// Argument is a value a command asks for before it runs, like a Raycast
// command argument. goto shows a prompt for it when the command is chosen.
type Argument struct {
	// Name is how the command reads it with Context.Argument, in kebab-case.
	Name        string
	Placeholder string
	Required    bool
}
