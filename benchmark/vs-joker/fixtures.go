package main

// Shared, in-package types and fixture data referenced (unqualified) by all
// three engines under comparison: the go-pug interpreter (via the
// map[string]any variants below), go-pug's generated codegen functions (the
// DataType is one of these structs), and Joker/jade's precompiled functions
// (its :go:func preamble declares the same struct as its parameter type).
// Field shapes and values are copied verbatim from ../data.go so all three
// engines render the exact same dataset.

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

func cardListFixture() cardListData {
	return cardListData{
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
	}
}

func tableFixture() tableData {
	return tableData{
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
	}
}

func formFixture() formData {
	return formData{
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
	}
}

func blogFixture() blogData {
	return blogData{
		PageTitle: "Recent Posts",
		Posts: []post{
			{Title: "Why We Rewrote Our Renderer", Author: "Priya N.", Category: "tech", Summary: "A look at the tradeoffs behind moving off a runtime-interpreted template engine."},
			{Title: "A Weekend in the Mountains", Author: "Sam O.", Category: "life", Summary: "Notes from a three-day backpacking trip, gear list included."},
			{Title: "Benchmarking Done Right", Author: "Priya N.", Category: "tech", Summary: "Why pre-compiling and warmup matter more than most benchmark writeups admit."},
			{Title: "Team Offsite Recap", Author: "Jordan K.", Category: "general", Summary: "Photos and highlights from this years offsite."},
			{Title: "Cooking for a Crowd", Author: "Sam O.", Category: "life", Summary: "Batch recipes that scale from four people to forty."},
			{Title: "Type Systems & Codegen", Author: "Priya N.", Category: "tech", Summary: "What a typed code generator can prove that a dynamic interpreter cannot."},
		},
	}
}

// The interpreter takes map[string]any locals rather than a typed struct, so
// each *Map function below builds the same data by hand from the typed
// fixture above.

func cardListMap() map[string]any {
	f := cardListFixture()
	prods := make([]any, len(f.Products))
	for i, p := range f.Products {
		prods[i] = map[string]any{"Name": p.Name, "Description": p.Description, "Price": p.Price, "InStock": p.InStock, "Featured": p.Featured}
	}
	return map[string]any{"PageTitle": f.PageTitle, "Products": prods}
}

func tableMap() map[string]any {
	f := tableFixture()
	emps := make([]any, len(f.Employees))
	for i, e := range f.Employees {
		emps[i] = map[string]any{"Name": e.Name, "Department": e.Department, "Status": e.Status, "Salary": e.Salary, "Highlight": e.Highlight}
	}
	return map[string]any{"PageTitle": f.PageTitle, "Employees": emps}
}

func formMap() map[string]any {
	f := formFixture()
	fields := make([]any, len(f.Fields))
	for i, x := range f.Fields {
		fields[i] = map[string]any{"ID": x.ID, "Label": x.Label, "Value": x.Value}
	}
	roles := make([]any, len(f.RoleOptions))
	for i, x := range f.RoleOptions {
		roles[i] = map[string]any{"Value": x.Value, "Label": x.Label}
	}
	return map[string]any{"Fields": fields, "RoleOptions": roles}
}

func blogMap() map[string]any {
	f := blogFixture()
	posts := make([]any, len(f.Posts))
	for i, p := range f.Posts {
		posts[i] = map[string]any{"Title": p.Title, "Author": p.Author, "Category": p.Category, "Summary": p.Summary}
	}
	return map[string]any{"PageTitle": f.PageTitle, "Posts": posts}
}
