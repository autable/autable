package nodes

import (
	"autable/internal/history"
	"autable/internal/workflow"
	"autable/internal/workflow/nodes/autable"
	dingtalkapprovalcreate "autable/internal/workflow/nodes/dingtalk/approval/create"
	"autable/internal/workflow/nodes/dingtalk/notable/listrecords"
	"autable/internal/workflow/nodes/dingtalk/robot"
	batchsendoto "autable/internal/workflow/nodes/dingtalk/robot/batchsendoto"
	"autable/internal/workflow/nodes/echo"
	githubcontent "autable/internal/workflow/nodes/github/file/content"
	kingdeepurchaseorderlist "autable/internal/workflow/nodes/kingdee/purchaseorder/list"
	"autable/internal/workflow/nodes/table/field"
	"autable/internal/workflow/nodes/table/recordchanged"
	rowcreate "autable/internal/workflow/nodes/table/row/create"
	rowdelete "autable/internal/workflow/nodes/table/row/delete"
	rowlist "autable/internal/workflow/nodes/table/row/list"
	rowquery "autable/internal/workflow/nodes/table/row/query"
	rowupdate "autable/internal/workflow/nodes/table/row/update"
	rowupsert "autable/internal/workflow/nodes/table/row/upsert"
	"autable/internal/workflow/nodes/time/schedule"
	webhooktrigger "autable/internal/workflow/nodes/webhook/trigger"
)

type Dependencies struct {
	History history.Store
	Autable autable.Service
}

func All(deps Dependencies) []workflow.Node {
	nodes := append(Remote(),
		recordchanged.NewNode(deps.History),
		schedule.Node{},
		webhooktrigger.Node{},
	)
	nodes = append(nodes, AutableNodes(deps.Autable)...)
	return nodes
}

// Remote returns the nodes a remote runner can execute: every node
// constructible without server-side dependencies.
func Remote() []workflow.Node {
	return []workflow.Node{
		echo.Node{},
		robot.NewNode(),
		dingtalkapprovalcreate.NewNode(),
		listrecords.NewNode(),
		batchsendoto.NewNode(),
		githubcontent.NewNode(),
		kingdeepurchaseorderlist.NewNode(),
	}
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
		rowquery.NewNode(service),
		field.NewCreateNode(service),
	}
}
