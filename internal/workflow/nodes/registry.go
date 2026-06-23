package nodes

import (
	"autable/internal/history"
	"autable/internal/workflow"
	"autable/internal/workflow/nodes/autable"
	"autable/internal/workflow/nodes/dingtalk/notable/listrecords"
	"autable/internal/workflow/nodes/dingtalk/robot"
	"autable/internal/workflow/nodes/echo"
	githubcontent "autable/internal/workflow/nodes/github/file/content"
	"autable/internal/workflow/nodes/table/field"
	"autable/internal/workflow/nodes/table/recordchanged"
	rowcreate "autable/internal/workflow/nodes/table/row/create"
	rowdelete "autable/internal/workflow/nodes/table/row/delete"
	rowlist "autable/internal/workflow/nodes/table/row/list"
	rowupdate "autable/internal/workflow/nodes/table/row/update"
	rowupsert "autable/internal/workflow/nodes/table/row/upsert"
	"autable/internal/workflow/nodes/time/schedule"
)

type Dependencies struct {
	History history.Store
	Autable autable.Service
}

func All(deps Dependencies) []workflow.Node {
	nodes := []workflow.Node{
		echo.Node{},
		recordchanged.NewNode(deps.History),
		schedule.Node{},
		robot.NewNode(),
		listrecords.NewNode(),
		githubcontent.NewNode(),
	}
	nodes = append(nodes, AutableNodes(deps.Autable)...)
	return nodes
}

func AutableNodes(service autable.Service) []workflow.Node {
	if service == nil {
		return nil
	}
	return []workflow.Node{
		rowcreate.NewNode(service),
		rowupdate.NewNode(service),
		rowupsert.NewNode(service),
		rowdelete.NewNode(service),
		rowlist.NewNode(service),
		field.NewCreateNode(service),
	}
}
