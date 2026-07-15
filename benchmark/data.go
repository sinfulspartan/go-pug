package main

import "reflect"

// benchCase is one template's complete, self-contained fixture: the
// interpreter/pug.js data (a plain map, marshaled to JSON for the Node leg
// and passed straight to the interpreter), and — when the template is
// codegen-supported — the typed Go counterpart GenerateGo needs: the
// reflect.Type it resolves field expressions against, the verbatim
// declaration source of that type (spliced into the throwaway module the
// generated function is built in), and a Go source literal of a fixture
// value of that type. The map and the Go literal describe the SAME logical
// dataset by construction — every field below is set once per template and
// mirrored by hand into both shapes, exactly as
// perf-compare/tri-diff/cases_synth.go's fixtures already do.
type benchCase struct {
	name        string
	description string
	template    string // filename under templates/, e.g. "card_list.pug"

	interpData map[string]any

	dataType       string
	reflectType    reflect.Type
	structSrc      string
	dataLiteralSrc string
}

// --- mixin_data_args.pug ---

type mixinArgsUser struct {
	Name string
	Age  int
}

type mixinArgsData struct {
	User mixinArgsUser
}

// --- mixin_default.pug (data-free) ---

type emptyData struct{}

// --- mixin_attrs.pug ---

type mixinAttrsItem struct {
	Label string
}

type mixinAttrsData struct {
	Item mixinAttrsItem
}

// --- nil_pointer_path.pug ---

type npProfile struct {
	Name string
}

type npUser struct {
	Profile *npProfile
}

type nilPathData struct {
	User npUser
}

// --- card_list.pug ---

type product struct {
	Name        string
	Description string
	Price       string
	InStock     bool
	Featured    bool
}

type cardListData struct {
	PageTitle string
	Products  []product
}

// --- table.pug ---

type employee struct {
	Name       string
	Department string
	Status     string
	Salary     string
	Highlight  bool
}

type tableData struct {
	PageTitle string
	Employees []employee
}

// --- form.pug ---

type fieldSpec struct {
	ID    string
	Label string
	Value string
}

type roleOption struct {
	Value string
	Label string
}

type formData struct {
	Fields      []fieldSpec
	RoleOptions []roleOption
}

// --- blog.pug ---

type post struct {
	Title    string
	Author   string
	Category string
	Summary  string
}

type blogData struct {
	PageTitle string
	Posts     []post
}

func benchCases() []benchCase {
	return []benchCase{
		{
			name:        "mixin_data_args",
			description: "mixin called with struct-typed data (positional string args)",
			template:    "mixin_data_args.pug",
			interpData: map[string]any{
				"user": map[string]any{"Name": "Alice", "Age": 30},
			},
			dataType:    "mixinArgsData",
			reflectType: reflect.TypeOf(mixinArgsData{}),
			structSrc: `type mixinArgsUser struct {
	Name string
	Age  int
}

type mixinArgsData struct {
	User mixinArgsUser
}
`,
			dataLiteralSrc: `mixinArgsData{User: mixinArgsUser{Name: "Alice", Age: 30}}`,
		},
		{
			name:           "mixin_default",
			description:    "mixin with a default parameter, called twice (data-free)",
			template:       "mixin_default.pug",
			interpData:     map[string]any{},
			dataType:       "emptyData",
			reflectType:    reflect.TypeOf(emptyData{}),
			structSrc:      "type emptyData struct{}\n",
			dataLiteralSrc: "emptyData{}",
		},
		{
			name:        "mixin_attrs",
			description: "mixin with &attributes spreading a caller-supplied attribute block",
			template:    "mixin_attrs.pug",
			interpData: map[string]any{
				"item": map[string]any{"Label": "Chip label"},
			},
			dataType:    "mixinAttrsData",
			reflectType: reflect.TypeOf(mixinAttrsData{}),
			structSrc: `type mixinAttrsItem struct {
	Label string
}

type mixinAttrsData struct {
	Item mixinAttrsItem
}
`,
			dataLiteralSrc: `mixinAttrsData{Item: mixinAttrsItem{Label: "Chip label"}}`,
		},
		{
			name:        "nil_pointer_path",
			description: "nil-safe dot-path through a non-nil pointer intermediate",
			template:    "nil_pointer_path.pug",
			interpData: map[string]any{
				"user": map[string]any{"Profile": map[string]any{"Name": "Bob"}},
			},
			dataType:    "nilPathData",
			reflectType: reflect.TypeOf(nilPathData{}),
			structSrc: `type npProfile struct {
	Name string
}

type npUser struct {
	Profile *npProfile
}

type nilPathData struct {
	User npUser
}
`,
			dataLiteralSrc: `nilPathData{User: npUser{Profile: &npProfile{Name: "Bob"}}}`,
		},
		{
			name:        "card_list",
			description: "product grid: each-loop, paren-ternary dynamic class, if/else badge",
			template:    "card_list.pug",
			interpData: map[string]any{
				"PageTitle": "Featured Products",
				"Products": []any{
					map[string]any{"Name": "Trail Runner Jacket", "Description": "Lightweight & water-resistant, packs into its own pocket", "Price": "$89.00", "InStock": true, "Featured": true},
					map[string]any{"Name": "Merino Wool Socks (3-pack)", "Description": "Odor-resistant, cushioned heel & toe", "Price": "$24.00", "InStock": true, "Featured": false},
					map[string]any{"Name": "Camp Stove", "Description": "Compact isobutane stove, boils 1L in under 3 minutes", "Price": "$54.50", "InStock": false, "Featured": false},
					map[string]any{"Name": "Insulated Bottle 32oz", "Description": "Keeps drinks cold 24h / hot 12h", "Price": "$32.00", "InStock": true, "Featured": true},
					map[string]any{"Name": "Trekking Poles (pair)", "Description": "Carbon fiber, adjustable 24in - 55in", "Price": "$79.99", "InStock": true, "Featured": false},
					map[string]any{"Name": "Headlamp 350lm", "Description": "Rechargeable, red night-vision mode", "Price": "$38.00", "InStock": false, "Featured": false},
					map[string]any{"Name": "Backpack 45L", "Description": "Internal frame, rain cover included", "Price": "$149.00", "InStock": true, "Featured": true},
					map[string]any{"Name": "Sleeping Bag 20F", "Description": "Down fill, compresses to a 8in x 12in stuff sack", "Price": "$210.00", "InStock": true, "Featured": false},
					map[string]any{"Name": "Water Filter Straw", "Description": "Filters 99.99% of bacteria & protozoa, 1000L rated", "Price": "$19.95", "InStock": true, "Featured": false},
					map[string]any{"Name": "Camp Chair", "Description": "Packable aluminum frame, 300lb capacity", "Price": "$44.00", "InStock": false, "Featured": false},
				},
			},
			dataType:    "cardListData",
			reflectType: reflect.TypeOf(cardListData{}),
			structSrc: `type product struct {
	Name        string
	Description string
	Price       string
	InStock     bool
	Featured    bool
}

type cardListData struct {
	PageTitle string
	Products  []product
}
`,
			dataLiteralSrc: `cardListData{
	PageTitle: "Featured Products",
	Products: []product{
		{Name: "Trail Runner Jacket", Description: "Lightweight & water-resistant, packs into its own pocket", Price: "$89.00", InStock: true, Featured: true},
		{Name: "Merino Wool Socks (3-pack)", Description: "Odor-resistant, cushioned heel & toe", Price: "$24.00", InStock: true, Featured: false},
		{Name: "Camp Stove", Description: "Compact isobutane stove, boils 1L in under 3 minutes", Price: "$54.50", InStock: false, Featured: false},
		{Name: "Insulated Bottle 32oz", Description: "Keeps drinks cold 24h / hot 12h", Price: "$32.00", InStock: true, Featured: true},
		{Name: "Trekking Poles (pair)", Description: "Carbon fiber, adjustable 24in - 55in", Price: "$79.99", InStock: true, Featured: false},
		{Name: "Headlamp 350lm", Description: "Rechargeable, red night-vision mode", Price: "$38.00", InStock: false, Featured: false},
		{Name: "Backpack 45L", Description: "Internal frame, rain cover included", Price: "$149.00", InStock: true, Featured: true},
		{Name: "Sleeping Bag 20F", Description: "Down fill, compresses to a 8in x 12in stuff sack", Price: "$210.00", InStock: true, Featured: false},
		{Name: "Water Filter Straw", Description: "Filters 99.99% of bacteria & protozoa, 1000L rated", Price: "$19.95", InStock: true, Featured: false},
		{Name: "Camp Chair", Description: "Packable aluminum frame, 300lb capacity", Price: "$44.00", InStock: false, Featured: false},
	},
}`,
		},
		{
			name:        "table",
			description: "data table: each-loop over rows, class-object zebra highlight",
			template:    "table.pug",
			interpData: map[string]any{
				"PageTitle": "Staff Directory",
				"Employees": []any{
					map[string]any{"Name": "Grace Hopper", "Department": "Engineering", "Status": "Active", "Salary": "$142,000", "Highlight": false},
					map[string]any{"Name": "Ada Lovelace", "Department": "Engineering", "Status": "Active", "Salary": "$138,500", "Highlight": true},
					map[string]any{"Name": "Alan Turing", "Department": "Research", "Status": "Active", "Salary": "$151,200", "Highlight": false},
					map[string]any{"Name": "Katherine Johnson", "Department": "Research", "Status": "On Leave", "Salary": "$144,900", "Highlight": true},
					map[string]any{"Name": "Margaret Hamilton", "Department": "Engineering", "Status": "Active", "Salary": "$147,300", "Highlight": false},
					map[string]any{"Name": "Radia Perlman", "Department": "Infrastructure", "Status": "Active", "Salary": "$139,750", "Highlight": true},
					map[string]any{"Name": "Barbara Liskov", "Department": "Research", "Status": "Active", "Salary": "$153,000", "Highlight": false},
					map[string]any{"Name": "Frances Allen", "Department": "Engineering", "Status": "Retired", "Salary": "$0", "Highlight": false},
					map[string]any{"Name": "Shafi Goldwasser", "Department": "Research", "Status": "Active", "Salary": "$149,400", "Highlight": true},
					map[string]any{"Name": "Jean Bartik", "Department": "Engineering", "Status": "Active", "Salary": "$136,200", "Highlight": false},
					map[string]any{"Name": "Adele Goldberg", "Department": "Product", "Status": "Active", "Salary": "$140,000", "Highlight": true},
					map[string]any{"Name": "Karen Sparck Jones", "Department": "Research", "Status": "Active", "Salary": "$145,600", "Highlight": false},
					map[string]any{"Name": "Mary Kenneth Keller", "Department": "Product", "Status": "On Leave", "Salary": "$133,800", "Highlight": false},
					map[string]any{"Name": "Elizabeth Feinler", "Department": "Infrastructure", "Status": "Active", "Salary": "$137,100", "Highlight": true},
					map[string]any{"Name": "Annie Easley", "Department": "Research", "Status": "Active", "Salary": "$146,700", "Highlight": false},
				},
			},
			dataType:    "tableData",
			reflectType: reflect.TypeOf(tableData{}),
			structSrc: `type employee struct {
	Name       string
	Department string
	Status     string
	Salary     string
	Highlight  bool
}

type tableData struct {
	PageTitle string
	Employees []employee
}
`,
			dataLiteralSrc: `tableData{
	PageTitle: "Staff Directory",
	Employees: []employee{
		{Name: "Grace Hopper", Department: "Engineering", Status: "Active", Salary: "$142,000", Highlight: false},
		{Name: "Ada Lovelace", Department: "Engineering", Status: "Active", Salary: "$138,500", Highlight: true},
		{Name: "Alan Turing", Department: "Research", Status: "Active", Salary: "$151,200", Highlight: false},
		{Name: "Katherine Johnson", Department: "Research", Status: "On Leave", Salary: "$144,900", Highlight: true},
		{Name: "Margaret Hamilton", Department: "Engineering", Status: "Active", Salary: "$147,300", Highlight: false},
		{Name: "Radia Perlman", Department: "Infrastructure", Status: "Active", Salary: "$139,750", Highlight: true},
		{Name: "Barbara Liskov", Department: "Research", Status: "Active", Salary: "$153,000", Highlight: false},
		{Name: "Frances Allen", Department: "Engineering", Status: "Retired", Salary: "$0", Highlight: false},
		{Name: "Shafi Goldwasser", Department: "Research", Status: "Active", Salary: "$149,400", Highlight: true},
		{Name: "Jean Bartik", Department: "Engineering", Status: "Active", Salary: "$136,200", Highlight: false},
		{Name: "Adele Goldberg", Department: "Product", Status: "Active", Salary: "$140,000", Highlight: true},
		{Name: "Karen Sparck Jones", Department: "Research", Status: "Active", Salary: "$145,600", Highlight: false},
		{Name: "Mary Kenneth Keller", Department: "Product", Status: "On Leave", Salary: "$133,800", Highlight: false},
		{Name: "Elizabeth Feinler", Department: "Infrastructure", Status: "Active", Salary: "$137,100", Highlight: true},
		{Name: "Annie Easley", Department: "Research", Status: "Active", Salary: "$146,700", Highlight: false},
	},
}`,
		},
		{
			name:        "form",
			description: "settings form: each-loop text fields + each-loop select options",
			template:    "form.pug",
			interpData: map[string]any{
				"Fields": []any{
					map[string]any{"ID": "display-name", "Label": "Display name", "Value": "jrivera"},
					map[string]any{"ID": "email", "Label": "Email address", "Value": "jrivera@example.com"},
					map[string]any{"ID": "phone", "Label": "Phone number", "Value": "555-0142"},
					map[string]any{"ID": "timezone", "Label": "Timezone", "Value": "America/Denver"},
					map[string]any{"ID": "company", "Label": "Company", "Value": "Acme & Sons Co."},
				},
				"RoleOptions": []any{
					map[string]any{"Value": "viewer", "Label": "Viewer"},
					map[string]any{"Value": "editor", "Label": "Editor"},
					map[string]any{"Value": "admin", "Label": "Administrator"},
				},
			},
			dataType:    "formData",
			reflectType: reflect.TypeOf(formData{}),
			structSrc: `type fieldSpec struct {
	ID    string
	Label string
	Value string
}

type roleOption struct {
	Value string
	Label string
}

type formData struct {
	Fields      []fieldSpec
	RoleOptions []roleOption
}
`,
			dataLiteralSrc: `formData{
	Fields: []fieldSpec{
		{ID: "display-name", Label: "Display name", Value: "jrivera"},
		{ID: "email", Label: "Email address", Value: "jrivera@example.com"},
		{ID: "phone", Label: "Phone number", Value: "555-0142"},
		{ID: "timezone", Label: "Timezone", Value: "America/Denver"},
		{ID: "company", Label: "Company", Value: "Acme & Sons Co."},
	},
	RoleOptions: []roleOption{
		{Value: "viewer", Label: "Viewer"},
		{Value: "editor", Label: "Editor"},
		{Value: "admin", Label: "Administrator"},
	},
}`,
		},
		{
			name:        "blog",
			description: "blog index: each-loop with a nested case/when tag classifier",
			template:    "blog.pug",
			interpData: map[string]any{
				"PageTitle": "Recent Posts",
				"Posts": []any{
					map[string]any{"Title": "Why We Rewrote Our Renderer", "Author": "Priya N.", "Category": "tech", "Summary": "A look at the tradeoffs behind moving off a runtime-interpreted template engine."},
					map[string]any{"Title": "A Weekend in the Mountains", "Author": "Sam O.", "Category": "life", "Summary": "Notes from a three-day backpacking trip, gear list included."},
					map[string]any{"Title": "Benchmarking Done Right", "Author": "Priya N.", "Category": "tech", "Summary": "Why pre-compiling and warmup matter more than most benchmark writeups admit."},
					map[string]any{"Title": "Team Offsite Recap", "Author": "Jordan K.", "Category": "general", "Summary": "Photos and highlights from this years offsite."},
					map[string]any{"Title": "Cooking for a Crowd", "Author": "Sam O.", "Category": "life", "Summary": "Batch recipes that scale from four people to forty."},
					map[string]any{"Title": "Type Systems & Codegen", "Author": "Priya N.", "Category": "tech", "Summary": "What a typed code generator can prove that a dynamic interpreter cannot."},
				},
			},
			dataType:    "blogData",
			reflectType: reflect.TypeOf(blogData{}),
			structSrc: `type post struct {
	Title    string
	Author   string
	Category string
	Summary  string
}

type blogData struct {
	PageTitle string
	Posts     []post
}
`,
			dataLiteralSrc: `blogData{
	PageTitle: "Recent Posts",
	Posts: []post{
		{Title: "Why We Rewrote Our Renderer", Author: "Priya N.", Category: "tech", Summary: "A look at the tradeoffs behind moving off a runtime-interpreted template engine."},
		{Title: "A Weekend in the Mountains", Author: "Sam O.", Category: "life", Summary: "Notes from a three-day backpacking trip, gear list included."},
		{Title: "Benchmarking Done Right", Author: "Priya N.", Category: "tech", Summary: "Why pre-compiling and warmup matter more than most benchmark writeups admit."},
		{Title: "Team Offsite Recap", Author: "Jordan K.", Category: "general", Summary: "Photos and highlights from this years offsite."},
		{Title: "Cooking for a Crowd", Author: "Sam O.", Category: "life", Summary: "Batch recipes that scale from four people to forty."},
		{Title: "Type Systems & Codegen", Author: "Priya N.", Category: "tech", Summary: "What a typed code generator can prove that a dynamic interpreter cannot."},
	},
}`,
		},
	}
}
