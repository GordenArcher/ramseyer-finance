package db

// categorySeed is the durable definition used to create the congregation's standard
// chart of accounts. I keep the statement metadata beside each account because the
// reporting destination is part of the account's accounting meaning; deriving it later
// from a display name would make harmless renames capable of moving balances between
// financial-statement sections.
type categorySeed struct {
	CategoryType  string
	Name          string
	Parent        string
	NoteRef       string
	ReportSection string
}

// workbookChartCategories mirrors the updated PCG standard workbook. Top-level rows are
// the financial-statement lines and child rows are the detailed Trial Balance accounts.
// Keeping this as structured data gives fresh databases and upgrade migrations one shared
// source of truth, preventing the two installation paths from drifting apart.
func workbookChartCategories() []categorySeed {
	return []categorySeed{
		// INCOME
		{"income", "Tithes", "", "3", ""},
		{"income", "Offerings", "", "4", ""},
		{"income", "Adult Service Offertory", "Offerings", "4", ""},
		{"income", "Welfare Offertory", "Offerings", "4", ""},
		{"income", "JY Offerings", "Offerings", "4", ""},
		{"income", "Children's Service Offerings", "Offerings", "4", ""},
		{"income", "Voluntary Thanks Offerings (VTO)", "Offerings", "4", ""},
		{"income", "Prayer Meetings Offerings", "Offerings", "4", ""},
		{"income", "Altar Offerings", "Offerings", "4", ""},
		{"income", "Revival Offertories", "Offerings", "4", ""},
		{"income", "Evening Services Offerings", "Offerings", "4", ""},
		{"income", "1st Money to God Offerings", "Offerings", "4", ""},
		{"income", "Marriage & Funeral Services Offerings", "Offerings", "4", ""},
		{"income", "31st Night & Carol Service Offerings", "Offerings", "4", ""},

		{"income", "Harvest Proceeds", "", "5", ""},
		{"income", "Harvest Launch", "Harvest Proceeds", "5", ""},
		{"income", "Pledges Redeemed", "Harvest Proceeds", "5", ""},
		{"income", "Mini Harvests", "Harvest Proceeds", "5", ""},
		{"income", "Annual/Main Harvest", "Harvest Proceeds", "5", ""},
		{"income", "Children's Harvest", "Harvest Proceeds", "5", ""},

		{"income", "Donations Received", "", "6", ""},
		{"income", "Donations from Members", "Donations Received", "6", ""},
		{"income", "Donations from Other PCG Courts", "Donations Received", "6", ""},
		{"income", "Special Appeals", "Donations Received", "6", ""},

		{"income", "Investment Income", "", "7", ""},
		{"income", "Income from Fin. Assets: Treasury Bill", "Investment Income", "7", ""},
		{"income", "Income from Fin. Assets: Fixed Deposits", "Investment Income", "7", ""},
		{"income", "Others", "Investment Income", "7", ""},

		{"income", "Other Income", "", "8", ""},
		{"income", "Bus Rental Income", "Other Income", "8", ""},
		{"income", "Auditorium Rental Income", "Other Income", "8", ""},
		{"income", "Interest on Savings Account", "Other Income", "8", ""},
		{"income", "Profit on Sale of Anniversary Cloth", "Other Income", "8", ""},
		{"income", "Almanac / Statutory Days' Collections", "Other Income", "8", ""},
		{"income", "Communion/Eucharist", "Other Income", "8", ""},
		{"income", "Exchange Gain", "Other Income", "8", ""},
		{"income", "Sale of Worship Materials, etc.", "Other Income", "8", ""},
		{"income", "Sundry Receipts", "Other Income", "8", ""},

		// EXPENDITURE
		{"expenditure", "Harvest Expenses", "", "5", ""},
		{"expenditure", "Contributions Paid", "", "9", ""},
		{"expenditure", "50% Contribution on General Income", "Contributions Paid", "9", ""},
		{"expenditure", "10% Contribution on Harvest Income", "Contributions Paid", "9", ""},

		{"expenditure", "Agents' Expenses", "", "10", ""},
		{"expenditure", "Ministers' Duty Allowance", "Agents' Expenses", "10", ""},
		{"expenditure", "Catechists' Duty Allowance", "Agents' Expenses", "10", ""},
		{"expenditure", "Fuel Allowance", "Agents' Expenses", "10", ""},
		{"expenditure", "Leave Allowance", "Agents' Expenses", "10", ""},
		{"expenditure", "Hospitality Allowance", "Agents' Expenses", "10", ""},
		{"expenditure", "Utilities-Manse", "Agents' Expenses", "10", ""},
		{"expenditure", "Medical Expenses", "Agents' Expenses", "10", ""},
		{"expenditure", "Manse Expenses (Rent)", "Agents' Expenses", "10", ""},
		{"expenditure", "Other Allowances (Agents)", "Agents' Expenses", "10", ""},

		{"expenditure", "Staff Cost", "", "11", ""},
		{"expenditure", "Basic Salaries", "Staff Cost", "11", ""},
		{"expenditure", "Pension - 13% Employer 's Contributions", "Staff Cost", "11", ""},
		{"expenditure", "Leave Allowance", "Staff Cost", "11", ""},
		{"expenditure", "Medical Expenses", "Staff Cost", "11", ""},
		{"expenditure", "Training & Development", "Staff Cost", "11", ""},

		{"expenditure", "Other Allowances", "", "12", ""},
		{"expenditure", "Senior Presbyter", "Other Allowances", "12", ""},
		{"expenditure", "Session Clerk", "Other Allowances", "12", ""},
		{"expenditure", "Chapel Keeper", "Other Allowances", "12", ""},
		{"expenditure", "Treasurer", "Other Allowances", "12", ""},
		{"expenditure", "Organist", "Other Allowances", "12", ""},
		{"expenditure", "Sound Technician", "Other Allowances", "12", ""},
		{"expenditure", "Cleaners", "Other Allowances", "12", ""},
		{"expenditure", "Retired Agents", "Other Allowances", "12", ""},
		{"expenditure", "Choir Master", "Other Allowances", "12", ""},
		{"expenditure", "Others", "Other Allowances", "12", ""},

		{"expenditure", "Evangelism Expenses", "", "13", ""},
		{"expenditure", "Palm Sunday/Easter/Emmaus Day Activities", "Evangelism Expenses", "13", ""},
		{"expenditure", "Other Evangelistic Activities (Radio)", "Evangelism Expenses", "13", ""},
		{"expenditure", "House-to-house Evangelism", "Evangelism Expenses", "13", ""},
		{"expenditure", "Hospital and Police Cells Ministry", "Evangelism Expenses", "13", ""},
		{"expenditure", "Tracts / Bibles Purchased", "Evangelism Expenses", "13", ""},
		{"expenditure", "Evangelism Courses/Trainings", "Evangelism Expenses", "13", ""},

		{"expenditure", "Group & Committee Expenses", "", "14", ""},
		{"expenditure", "Children's Service", "Group & Committee Expenses", "14", ""},
		{"expenditure", "J. Y.", "Group & Committee Expenses", "14", ""},
		{"expenditure", "YPG", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Brigade", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Church Choir", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Choral Band", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Mission & Evangelism", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Church Life & Nurture", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Dev't & Social Services", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Education", "Group & Committee Expenses", "14", ""},
		{"expenditure", "Finance", "Group & Committee Expenses", "14", ""},

		{"expenditure", "Meetings and Conferences", "", "15", ""},
		{"expenditure", "Ministers' Conference", "Meetings and Conferences", "15", ""},
		{"expenditure", "Catechists' Conference", "Meetings and Conferences", "15", ""},
		{"expenditure", "Ministers' Spouses Conference", "Meetings and Conferences", "15", ""},
		{"expenditure", "Catechists' Spouses Conference", "Meetings and Conferences", "15", ""},
		{"expenditure", "Congregational Conference", "Meetings and Conferences", "15", ""},
		{"expenditure", "Session Meetings", "Meetings and Conferences", "15", ""},
		{"expenditure", "District Session", "Meetings and Conferences", "15", ""},
		{"expenditure", "Presbytery Session", "Meetings and Conferences", "15", ""},
		{"expenditure", "Others", "Meetings and Conferences", "15", ""},

		{"expenditure", "Training, Seminars, Workshops & Retreats", "", "16", ""},
		{"expenditure", "Ministerial Training - TTS", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Ministerial Training - SMT", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Commissioning", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Ordinations", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Catechists' Training", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Retreats", "Training, Seminars, Workshops & Retreats", "16", ""},
		{"expenditure", "Workshops & Seminars", "Training, Seminars, Workshops & Retreats", "16", ""},

		{"expenditure", "Social Services", "", "17", ""},
		{"expenditure", "Charitable Donations", "Social Services", "17", ""},
		{"expenditure", "Scholarships", "Social Services", "17", ""},
		{"expenditure", "Funeral Donations", "Social Services", "17", ""},
		{"expenditure", "Inductions/Introduction Donations", "Social Services", "17", ""},
		{"expenditure", "Send Off Donations", "Social Services", "17", ""},
		{"expenditure", "Members' Welfare Expenses", "Social Services", "17", ""},
		{"expenditure", "Honorarium", "Social Services", "17", ""},
		{"expenditure", "Invalids Visits/Communion Expenses", "Social Services", "17", ""},
		{"expenditure", "Presbyters' Dues", "Social Services", "17", ""},
		{"expenditure", "Donations to Other PCG Courts", "Social Services", "17", ""},
		{"expenditure", "Christmas Packages/Gifts", "Social Services", "17", ""},

		{"expenditure", "Levies Paid", "", "18", ""},
		{"expenditure", "GA Office Levies", "Levies Paid", "18", ""},
		{"expenditure", "Presbytery Levies", "Levies Paid", "18", ""},
		{"expenditure", "District Levies", "Levies Paid", "18", ""},
		{"expenditure", "Other Levies", "Levies Paid", "18", ""},

		{"expenditure", "Property Upkeep", "", "19", ""},
		{"expenditure", "Repairs & Maintenance - Chapel", "Property Upkeep", "19", ""},
		{"expenditure", "Repairs & Maintenance - Manse", "Property Upkeep", "19", ""},
		{"expenditure", "Repairs & Maintenance - Offices", "Property Upkeep", "19", ""},
		{"expenditure", "Cleaning & Sanitation", "Property Upkeep", "19", ""},
		{"expenditure", "Servicing of Fire Extinguishers", "Property Upkeep", "19", ""},
		{"expenditure", "Security Services", "Property Upkeep", "19", ""},
		{"expenditure", "Insurance on Chapel and Manse", "Property Upkeep", "19", ""},
		{"expenditure", "Land Documentations", "Property Upkeep", "19", ""},

		{"expenditure", "General Administration Expenses", "", "20", ""},
		{"expenditure", "Printing & Stationery", "General Administration Expenses", "20", ""},
		{"expenditure", "Utilities (Elect.,Water,Postal & Genset running)", "General Administration Expenses", "20", ""},
		{"expenditure", "Motor Vehicle Fuel", "General Administration Expenses", "20", ""},
		{"expenditure", "Motor Vehicle Maintenance", "General Administration Expenses", "20", ""},
		{"expenditure", "Motor Vehicle & Other Insurance", "General Administration Expenses", "20", ""},
		{"expenditure", "Communications (Telephone, Internet, DSTV etc.)", "General Administration Expenses", "20", ""},
		{"expenditure", "Rent Expenses", "General Administration Expenses", "20", ""},
		{"expenditure", "Equipment & Other Repairs & Maintenance", "General Administration Expenses", "20", ""},
		{"expenditure", "Accounting Services Fees", "General Administration Expenses", "20", ""},
		{"expenditure", "Travel & Transport", "General Administration Expenses", "20", ""},
		{"expenditure", "General Refreshments/Hospitality", "General Administration Expenses", "20", ""},
		{"expenditure", "Almanac /Statutory Payments", "General Administration Expenses", "20", ""},
		{"expenditure", "Refunds to Groups (Local/District)", "General Administration Expenses", "20", ""},
		{"expenditure", "Communion / Eucharist", "General Administration Expenses", "20", ""},
		{"expenditure", "Anniversary Expenses", "General Administration Expenses", "20", ""},
		{"expenditure", "Office Expenses", "General Administration Expenses", "20", ""},
		{"expenditure", "Audit Fees", "General Administration Expenses", "20", ""},
		{"expenditure", "Audit Expenses", "General Administration Expenses", "20", ""},
		{"expenditure", "Bank Charges", "General Administration Expenses", "20", ""},
		{"expenditure", "Others", "General Administration Expenses", "20", ""},

		{"expenditure", "Depreciation & Amortization Expenses", "", "21", ""},
		{"expenditure", "Depreciation Expense", "Depreciation & Amortization Expenses", "21", ""},
		{"expenditure", "Amortization Expense", "Depreciation & Amortization Expenses", "21", ""},

		// ASSETS
		{"asset", "Property, Plant & Equipment", "", "21", "non_current_asset"},
		{"asset", "Land", "Property, Plant & Equipment", "21", ""},
		{"asset", "Buildings - Chapel", "Property, Plant & Equipment", "21", ""},
		{"asset", "Buildings - Washroom", "Property, Plant & Equipment", "21", ""},
		{"asset", "Buildings - Manse with Outhouse", "Property, Plant & Equipment", "21", ""},
		{"asset", "Buildings - JY & Children's Chapel", "Property, Plant & Equipment", "21", ""},
		{"asset", "Motor Vehicles", "Property, Plant & Equipment", "21", ""},
		{"asset", "Power Generating Plant", "Property, Plant & Equipment", "21", ""},
		{"asset", "Furniture", "Property, Plant & Equipment", "21", ""},
		{"asset", "Fixtures and Fittings", "Property, Plant & Equipment", "21", ""},
		{"asset", "Office Equipment", "Property, Plant & Equipment", "21", ""},
		{"asset", "Computers & Accessories", "Property, Plant & Equipment", "21", ""},
		{"asset", "Church Instruments", "Property, Plant & Equipment", "21", ""},

		{"asset", "Long Term Investment", "", "22", "non_current_asset"},
		{"asset", "Investment in Water Project", "Long Term Investment", "22", ""},
		{"asset", "Shares in Credit Union", "Long Term Investment", "22", ""},
		{"asset", "Intangible Assets", "", "23", "non_current_asset"},
		{"asset", "Software", "Intangible Assets", "23", ""},

		{"asset", "Inventories", "", "24", "current_asset"},
		{"asset", "Merchandise - Anniversary Cloth", "Inventories", "24", ""},
		{"asset", "Stationery", "Inventories", "24", ""},
		{"asset", "Others", "Inventories", "24", ""},
		{"asset", "Accounts Receivable & Prepayments", "", "25", "current_asset"},
		{"asset", "Staff Salary Advances", "Accounts Receivable & Prepayments", "25", ""},
		{"asset", "Loans Granted", "Accounts Receivable & Prepayments", "25", ""},
		{"asset", "Other Receivables", "Accounts Receivable & Prepayments", "25", ""},

		{"asset", "Cash & Cash Equivalents", "", "26", "current_asset"},
		{"asset", "Cash on hand", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Petty Cash", "Cash & Cash Equivalents", "26", ""},
		{"asset", "GCB Bank Plc. Main - Current Accounts", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Societe General Ghana Plc. - Current Account", "Cash & Cash Equivalents", "26", ""},
		{"asset", "GCB Bank Ltd. - Savings Account", "Cash & Cash Equivalents", "26", ""},
		{"asset", "ADB Bank Plc - Current Account", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Credit Union - Savings", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Treasury Bills", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Fixed Deposits", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Others", "Cash & Cash Equivalents", "26", ""},
		// These three compatibility accounts preserve the previous desktop workflow and its
		// saved opening balances. New installations should prefer the detailed workbook bank
		// and cash accounts above, but upgrades must not strand historical Bank/Cash/Momo data.
		{"asset", "Bank", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Cash", "Cash & Cash Equivalents", "26", ""},
		{"asset", "Momo", "Cash & Cash Equivalents", "26", ""},

		// LIABILITIES
		{"liability", "Long Term Loan", "", "27", "long_term_liability"},
		{"liability", "Bridge Finance Loan - GCB Bank", "Long Term Loan", "27", ""},
		{"liability", "Accounts Payable & Accruals", "", "28", "current_liability"},
		{"liability", "District - Weekly Contributions", "Accounts Payable & Accruals", "28", ""},
		{"liability", "External Auditors", "Accounts Payable & Accruals", "28", ""},
		{"liability", "Ghana Revenue Authority - PAYE", "Accounts Payable & Accruals", "28", ""},
		{"liability", "Ghana Revenue Authority - WHT", "Accounts Payable & Accruals", "28", ""},
		{"liability", "SSNIT - Pension Tier 1", "Accounts Payable & Accruals", "28", ""},
		{"liability", "Pensions Trustee  - Tier 2", "Accounts Payable & Accruals", "28", ""},
		{"liability", "GWC Ltd.- Bills", "Accounts Payable & Accruals", "28", ""},
		{"liability", "Other Payables", "Accounts Payable & Accruals", "28", ""},
	}
}
