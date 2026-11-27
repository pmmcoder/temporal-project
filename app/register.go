package app

import (
	"personal-workflow-platform/app/approvaldemo"
	"personal-workflow-platform/app/greeting"
	"personal-workflow-platform/platform/sdk"
)

func Register(registry *sdk.Registry) {
	greeting.Register(registry)
	approvaldemo.Register(registry)
}
