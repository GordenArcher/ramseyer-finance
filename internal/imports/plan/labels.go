package plan

import "strings"

// categoryDefault is the built-in starting account for a workbook label. Standard entries
// are direct matches to the chart of accounts. Heuristic entries describe themed or
// project collections whose exact account is a judgement call, so the preview marks them
// for review and the operator can change them.
type categoryDefault struct {
	Category string
	Source   CategorySource
}

// labelAliases folds the spellings that mean the same collection onto one mapping key.
// Without this step a stray "SUNDAY OFFERING" line in one sheet would be treated as a new
// label and could be imported twice under a different choice.
var labelAliases = map[string]string{
	"SUNDAY SERVICE OFFERING": "ADULT SERVICE OFFERTORY",
	"SUNDAY OFFERING":         "ADULT SERVICE OFFERTORY",
	"FIRST SERVICE OFFERING":  "ADULT SERVICE OFFERTORY",
	"SECOND SERVICE OFFERING": "ADULT SERVICE OFFERTORY",
	"SECOND OFFERING":         "ADULT SERVICE OFFERTORY",
	"2ND OFFERING":            "ADULT SERVICE OFFERTORY",
	"2ND SERVICE":             "ADULT SERVICE OFFERTORY",

	"TITHE":  "TITHES",
	"TITHES": "TITHES",

	"JY OFFERING": "JY OFFERINGS",

	"CHILDRENS SERVICE OFFER":     "CHILDREN'S SERVICE OFFERINGS",
	"CHILDRENS SERV":              "CHILDREN'S SERVICE OFFERINGS",
	"CHILDREN'S SERVICE OFFER":    "CHILDREN'S SERVICE OFFERINGS",
	"CHILDREN'S SERVICE OFFERING": "CHILDREN'S SERVICE OFFERINGS",

	"FRIDAY EVENING PRAYERS": "PRAYER MEETINGS OFFERINGS",
	"FRIDAY OFFERING":        "PRAYER MEETINGS OFFERINGS",

	"WELFARE":          "WELFARE OFFERTORY",
	"WELFARE OFFERING": "WELFARE OFFERTORY",

	"REVIVAL":           "REVIVAL OFFERTORIES",
	"REVIVAL OFFERING":  "REVIVAL OFFERTORIES",
	"REVIVAL ENVELOPES": "REVIVAL OFFERTORIES",

	"MINI HARVEST":     "MINI HARVESTS",
	"4TH MINI HARVEST": "MINI HARVESTS",

	"CHILDRENS HARVEST": "CHILDREN'S HARVEST",

	"HARVEST":        "ANNUAL/MAIN HARVEST",
	"ANNUAL HARVEST": "ANNUAL/MAIN HARVEST",
	"MAIN HARVEST":   "ANNUAL/MAIN HARVEST",

	"DAY BORN":         "DAY BORN",
	"DAY BORNS":        "DAY BORN",
	"DAY BORN PLEDGES": "DAY BORN",

	"BIRTHDAY": "BIRTHDAY OFFERING",

	"COMMUNION TOLL": "COMMUNION/EUCHARIST",
	"COMMUNION":      "COMMUNION/EUCHARIST",

	"KUTUNSE PROJ":             "KUTUNSE PROJECT",
	"KUTUNSE PROJ OFFERING":    "KUTUNSE PROJECT",
	"KUTUNSE PROJECT":          "KUTUNSE PROJECT",
	"KUTUNSE PROJECT OFFERING": "KUTUNSE PROJECT",

	"FLOOD VICTIMS":          "FLOOD VICTIMS",
	"FLOOD VICTIMS-OFFERING": "FLOOD VICTIMS",

	"GMF OFFERING": "GMF OFFERING",
	"GMF SUNDAY":   "GMF OFFERING",

	"DISTRICT WOMEN'S FELLOWSHIP": "DISTRICT WOMEN'S FELLOWSHIP",
	"DISTRICT WOMENS OFFERING":    "DISTRICT WOMEN'S FELLOWSHIP",
}

// labelDefaults is the default account for every label the importer recognises. A label
// that is absent is deliberately undecided: the workbook uses it for a fund the church
// has not told us how to classify, so the preview asks instead of guessing.
var labelDefaults = map[string]categoryDefault{
	"ADULT SERVICE OFFERTORY":      {"Adult Service Offertory", CategoryStandard},
	"TITHES":                       {"Tithes", CategoryStandard},
	"VTO":                          {"Voluntary Thanks Offerings (VTO)", CategoryStandard},
	"JY OFFERINGS":                 {"JY Offerings", CategoryStandard},
	"CHILDREN'S SERVICE OFFERINGS": {"Children's Service Offerings", CategoryStandard},
	"PRAYER MEETINGS OFFERINGS":    {"Prayer Meetings Offerings", CategoryStandard},
	"WELFARE OFFERTORY":            {"Welfare Offertory", CategoryStandard},
	"REVIVAL OFFERTORIES":          {"Revival Offertories", CategoryStandard},
	"MINI HARVESTS":                {"Mini Harvests", CategoryStandard},
	"CHILDREN'S HARVEST":           {"Children's Harvest", CategoryStandard},
	"ANNUAL/MAIN HARVEST":          {"Annual/Main Harvest", CategoryStandard},
	"COMMUNION/EUCHARIST":          {"Communion/Eucharist", CategoryStandard},

	// Themed Sundays and project collections are real income, but the workbook does not
	// say whether the church treats them as regular offertory or as a special appeal.
	// Special Appeals is the closest chart account, and the preview flags every one.
	"AGED SUNDAY":                 {"Special Appeals", CategoryHeuristic},
	"ASCENSION DAY":               {"Special Appeals", CategoryHeuristic},
	"ASH WEDNESDAY":               {"Special Appeals", CategoryHeuristic},
	"BIBLE SUNDAY":                {"Special Appeals", CategoryHeuristic},
	"BIRTHDAY OFFERING":           {"Special Appeals", CategoryHeuristic},
	"BLUE CROSS SUNDAY":           {"Special Appeals", CategoryHeuristic},
	"CHILDREN SUNDAY":             {"Special Appeals", CategoryHeuristic},
	"DAY BORN":                    {"Special Appeals", CategoryHeuristic},
	"DISTRICT WOMEN'S FELLOWSHIP": {"Special Appeals", CategoryHeuristic},
	"EDUCATION SUNDAY":            {"Special Appeals", CategoryHeuristic},
	"EVANGELISM SUNDAY":           {"Special Appeals", CategoryHeuristic},
	"FLOOD VICTIMS":               {"Special Appeals", CategoryHeuristic},
	"GMF OFFERING":                {"Special Appeals", CategoryHeuristic},
	"HEALTH SUNDAY OFFERING":      {"Special Appeals", CategoryHeuristic},
	"KUTUNSE PROJECT":             {"Special Appeals", CategoryHeuristic},
	"MEN'S SUNDAY":                {"Special Appeals", CategoryHeuristic},
	"MUSIC SUNDAY":                {"Special Appeals", CategoryHeuristic},
	"PASSION WEEK":                {"Special Appeals", CategoryHeuristic},
	"PRESBYTERY SUNDAY":           {"Special Appeals", CategoryHeuristic},
	"SCHOLARSHIP":                 {"Special Appeals", CategoryHeuristic},
	"SPEAKING":                    {"Special Appeals", CategoryHeuristic},
	"WOMEN'S HARVEST":             {"Special Appeals", CategoryHeuristic},
	"WOMEN'S SUNDAY":              {"Special Appeals", CategoryHeuristic},
	"YAF THANKS OFFERING":         {"Voluntary Thanks Offerings (VTO)", CategoryHeuristic},
	"YOUTH & STUDENTS SUN":        {"JY Offerings", CategoryHeuristic},
	"YOUTH & STUDENTS SUNDAY":     {"JY Offerings", CategoryHeuristic},
	"YPG HARVEST":                 {"Special Appeals", CategoryHeuristic},
}

// CleanLabel prepares a workbook label for display. It keeps the original wording and
// capitalisation because that is what the operator sees in the workbook, but removes the
// stray spacing and punctuation that make two identical rows look different.
func CleanLabel(label string) string {
	cleaned := strings.ReplaceAll(label, "\u2019", "'")
	cleaned = strings.ReplaceAll(cleaned, "\u2018", "'")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimRight(cleaned, ":;,")
	return cleaned
}

// Key normalises a label into the stable mapping key used by the preview and the saved
// label choices. Dots are removed so V.T.O. and VTO share one key, and the alias table
// folds spelling variants such as SECOND  OFFERING onto their canonical name.
func Key(label string) string {
	normalised := strings.ToUpper(CleanLabel(label))
	normalised = strings.ReplaceAll(normalised, ".", "")
	normalised = strings.Join(strings.Fields(normalised), " ")

	if alias, exists := labelAliases[normalised]; exists {
		return alias
	}
	return normalised
}

// DefaultCategory returns the built-in account for a mapping key. When the key is not
// recognised the source is undecided and the caller must ask the operator before saving.
func DefaultCategory(key string) (string, CategorySource) {
	if fallback, exists := labelDefaults[key]; exists {
		return fallback.Category, fallback.Source
	}
	return "", CategoryUndecided
}
