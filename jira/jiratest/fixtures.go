package jiratest

import "time"

// Accounts of the canned sites.
const (
	// MiaID is the API user: the one Credentials() authenticates as.
	// Her profile zone is Europe/Berlin, the site's is America/Los_Angeles (C6).
	MiaID = "5b10a2844c20165700ede21g"
	// RaviID is the colleague the web-UI helpers act as; his email is hidden.
	RaviID = "5b10ac8d82e05b22cc7d4ef5"
	// JoID is a deactivated account.
	JoID = "557058:f58131cb-b67d-43c7-b30d-6b58d40bd077"
	// AutomationID is an app account.
	AutomationID = "557058:0867a421-a9ee-4659-801a-bc0ee4a4487e"

	MiaEmail = "mia@example.com"
	MiaToken = "mia-api-token"
)

// Well-known custom field ids of the company fixture.
const (
	FieldStartDate   = "customfield_10015"
	FieldStoryPoints = "customfield_10016"
	FieldRank        = "customfield_10019"
	FieldSprint      = "customfield_10020"
)

func siteUsers() []User {
	return []User{
		{AccountID: MiaID, DisplayName: "Mia Krystof", Email: MiaEmail,
			TimeZone: "Europe/Berlin", APIToken: MiaToken},
		{AccountID: RaviID, DisplayName: "Ravi Patel", Email: "ravi@example.com",
			EmailHidden: true, TimeZone: "Asia/Kolkata", ProjectAdmin: true},
		{AccountID: JoID, DisplayName: "Jo Doe", Email: "jo@example.com", Inactive: true},
		{AccountID: AutomationID, DisplayName: "Automation for Jira", AccountType: "app"},
	}
}

func sitePriorities() []Priority {
	return []Priority{
		{ID: "1", Name: "Highest", Color: "#d04437"},
		{ID: "2", Name: "High", Color: "#f15C75"},
		{ID: "3", Name: "Medium", Color: "#f79232", Default: true},
		{ID: "4", Name: "Low", Color: "#707070"},
		{ID: "5", Name: "Lowest", Color: "#999999"},
	}
}

func siteResolutions() []Resolution {
	return []Resolution{
		{ID: "10000", Name: "Done", Description: "Work has been completed on this issue."},
		{ID: "10001", Name: "Won't Do", Description: "This issue won't be actioned."},
		{ID: "10002", Name: "Duplicate", Description: "The problem is a duplicate of an existing issue."},
		{ID: "10003", Name: "Cannot Reproduce", Description: "This issue couldn't be reproduced."},
	}
}

func siteLinkTypes() []LinkType {
	return []LinkType{
		{ID: "10000", Name: "Blocks", Inward: "is blocked by", Outward: "blocks"},
		{ID: "10001", Name: "Cloners", Inward: "is cloned by", Outward: "clones"},
		{ID: "10002", Name: "Duplicate", Inward: "is duplicated by", Outward: "duplicates"},
		{ID: "10003", Name: "Relates", Inward: "relates to", Outward: "relates to"},
	}
}

// CompanySite is a company-managed ("classic") Jira Software site with one
// project, PROJ. Its workflow has a status whose name is not its category
// ("Ready for QA" is indeterminate, "Won't Do" is done), transitions that
// are not universal, and a resolution required on the way to Done.
func CompanySite() Site {
	const (
		todo    = "10000"
		inProg  = "3"
		qa      = "10001"
		done    = "10002"
		wontDo  = "10003"
		resDone = "10000"
		resWont = "10001"
	)
	work := Workflow{Initial: todo, Transitions: []Transition{
		{ID: "11", Name: "Start work", From: []string{todo}, To: inProg},
		{ID: "21", Name: "Ready for QA", From: []string{inProg}, To: qa},
		{ID: "61", Name: "Back to work", From: []string{qa}, To: inProg},
		{ID: "31", Name: "Done", From: []string{qa}, To: done,
			Screen: []TransitionField{{FieldID: "resolution", Required: true}}},
		{ID: "41", Name: "Reopen", From: []string{done, wontDo}, To: todo},
		{ID: "51", Name: "Won't Do", To: wontDo, Resolution: resWont},
	}}
	simple := Workflow{Initial: todo, Transitions: []Transition{
		{ID: "11", Name: "Start work", From: []string{todo}, To: inProg},
		{ID: "31", Name: "Done", From: []string{todo, inProg}, To: done, Resolution: resDone},
		{ID: "41", Name: "Reopen", From: []string{done}, To: todo},
	}}
	base := []string{"description", "priority", "assignee", "reporter", "labels", "duedate",
		"parent", FieldStartDate, FieldStoryPoints, FieldSprint, FieldRank}
	// Sprint and Rank are in the field context, not on the create screen,
	// which is how a classic project is usually set up.
	screen := []string{"description", "priority", "assignee", "reporter", "labels", "duedate",
		"parent", FieldStartDate, FieldStoryPoints}
	sprintStart := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

	return Site{
		Title:    "Example Company",
		TimeZone: "America/Los_Angeles",
		Me:       MiaID,
		WebUser:  RaviID,
		Users:    siteUsers(),
		Projects: []Project{{
			ID: "10000", Key: "PROJ", Name: "Project", Lead: RaviID,
			Statuses: []Status{
				{ID: todo, Name: "To Do", Category: CategoryNew},
				{ID: inProg, Name: "In Progress", Category: CategoryIndeterminate},
				{ID: qa, Name: "Ready for QA", Category: CategoryIndeterminate},
				{ID: done, Name: "Done", Category: CategoryDone},
				{ID: wontDo, Name: "Won't Do", Category: CategoryDone},
			},
			IssueTypes: []IssueType{
				{ID: "10000", Name: "Epic", HierarchyLevel: 1, Description: "A big user story that needs to be broken down.",
					Fields:   []string{"description", "priority", "assignee", "reporter", "labels", "duedate", FieldStartDate, FieldRank},
					Workflow: simple},
				{ID: "10001", Name: "Story", HierarchyLevel: 0, Description: "Functionality or a feature expressed as a user goal.",
					Fields: base, CreateScreen: screen, Workflow: work},
				{ID: "10002", Name: "Task", HierarchyLevel: 0, Description: "A small, distinct piece of work.",
					Fields: base, CreateScreen: screen, Workflow: work},
				{ID: "10004", Name: "Bug", HierarchyLevel: 0, Description: "A problem or error.",
					Fields: base, CreateScreen: screen, Required: []string{"priority"}, Workflow: work},
				{ID: "10003", Name: "Sub-task", HierarchyLevel: -1, Description: "A small piece of work that's part of a larger task.",
					Fields:   []string{"description", "priority", "assignee", "reporter", "labels", "duedate", "parent", FieldRank},
					Workflow: simple},
			},
			Sprints: []Sprint{
				{ID: 36, Name: "PROJ Sprint 3", State: "closed", BoardID: 5,
					Start: sprintStart.AddDate(0, 0, -14), End: sprintStart, Complete: sprintStart.Add(time.Hour)},
				{ID: 37, Name: "PROJ Sprint 4", State: "active", BoardID: 5, Goal: "Ship guest checkout",
					Start: sprintStart, End: sprintStart.AddDate(0, 0, 14)},
				{ID: 38, Name: "PROJ Sprint 5, the long one", State: "future", BoardID: 5},
			},
		}},
		Priorities:  sitePriorities(),
		Resolutions: siteResolutions(),
		LinkTypes:   siteLinkTypes(),
		Fields: []CustomField{
			{ID: FieldStartDate, Name: "Start date", Kind: KindDate},
			{ID: FieldStoryPoints, Name: "Story point estimate", Kind: KindNumber,
				Custom: "com.pyxis.greenhopper.jira:jsw-story-points"},
			{ID: FieldRank, Name: "Rank", Kind: KindRank},
			{ID: FieldSprint, Name: "Sprint", Kind: KindSprint},
		},
	}
}

// TeamSite is a team-managed (next-gen) site with one project, TEAM:
// simplified, project-scoped types and statuses, transitions from any
// status to any, and a project-scoped "Story point estimate" of type float
// (jira-api-vetting.md C10: the jsw key is not guaranteed).
func TeamSite() Site {
	const (
		todo   = "10030"
		inProg = "10031"
		done   = "10032"
	)
	free := Workflow{Initial: todo, Transitions: []Transition{
		{ID: "11", Name: "To Do", To: todo},
		{ID: "21", Name: "In Progress", To: inProg},
		{ID: "31", Name: "Done", To: done, Resolution: "10000"},
	}}
	const points = "customfield_10036"
	fields := []string{"description", "priority", "assignee", "reporter", "labels", "duedate",
		"parent", points, FieldRank}
	return Site{
		Title:    "Example Team",
		TimeZone: "America/Los_Angeles",
		Me:       MiaID,
		WebUser:  RaviID,
		Users:    siteUsers(),
		Projects: []Project{{
			ID: "10010", Key: "TEAM", Name: "Team", TeamManaged: true, Lead: MiaID,
			Statuses: []Status{
				{ID: todo, Name: "To Do", Category: CategoryNew},
				{ID: inProg, Name: "In Progress", Category: CategoryIndeterminate},
				{ID: done, Name: "Done", Category: CategoryDone},
			},
			IssueTypes: []IssueType{
				{ID: "10020", Name: "Epic", HierarchyLevel: 1, Fields: fields, Workflow: free},
				{ID: "10021", Name: "Story", HierarchyLevel: 0, Fields: fields, Workflow: free},
				{ID: "10022", Name: "Task", HierarchyLevel: 0, Fields: fields, Workflow: free},
				{ID: "10023", Name: "Bug", HierarchyLevel: 0, Fields: fields, Workflow: free},
				{ID: "10024", Name: "Subtask", HierarchyLevel: -1, Fields: fields, Workflow: free},
			},
		}},
		Priorities:  sitePriorities(),
		Resolutions: siteResolutions(),
		LinkTypes:   siteLinkTypes(),
		Fields: []CustomField{
			{ID: points, Name: "Story point estimate", Kind: KindNumber, ProjectID: "10010"},
			{ID: FieldRank, Name: "Rank", Kind: KindRank},
		},
	}
}
