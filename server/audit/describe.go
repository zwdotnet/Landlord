package audit

import (
	"net/http"
	"strconv"
	"strings"
)

// Only route metadata is used here. Never record request bodies: they may
// contain passwords, ID photos, invitations, or other private credentials.
func describeMutation(method, path string) (label, target string) {
	if i := strings.Index(path, "/api/"); i >= 0 {
		path = path[i+5:]
	}
	path = strings.TrimPrefix(path, "rental/")
	resources := map[string]string{
		"rooms": "房源", "tenants": "租户", "bills": "账单", "meter-records": "抄表记录",
		"fee-items": "收费项目", "payments": "收款记录", "ledgers": "台账", "users": "用户",
		"configs": "系统配置", "backups": "备份", "exports": "导出文件", "contracts": "租赁合同",
		"attachments": "租户附件", "room-items": "房内物品", "room-photos": "房源照片",
		"meter-photos": "抄表照片", "room-item-photos": "物品照片", "tenant-members": "同住人",
		"tenant-applications": "入住申请", "room-applications": "房源登记申请",
		"landlord-applications": "房东申请", "tenant-change-requests": "租户资料修改申请",
		"notifications": "站内消息", "profile": "个人资料", "security-questions": "安全问题",
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	resource := parts[0]
	target = resources[resource]
	if target == "" {
		target = "应用设置"
	}
	verb := map[string]string{http.MethodPost: "新增", http.MethodPut: "修改", http.MethodPatch: "修改", http.MethodDelete: "删除"}[method]
	label = verb + target
	last := parts[len(parts)-1]
	switch last {
	case "apply-link":
		label = "生成或重置租客登记链接"
		target = "房源"
	case "room-apply-link":
		label = "生成或重置房源登记链接"
		target = "台账"
	case "meter-records":
		if method == http.MethodPost {
			label = "保存抄表记录"
		}
	case "checkout":
		label = "办理租户退租"
	case "renew":
		label = "办理租户续租"
	case "pay":
		label = "登记账单收款"
	case "reverse":
		label = "冲销收款记录"
	case "recipients":
		label = "确认账单所属租户"
	case "recalc":
		label = "重新计算账单结转"
	case "generate":
		label = "批量生成账单"
	case "import":
		label = "导入" + target
	case "batch":
		label = "批量登记租户"
	case "parse-text":
		label = "识别租户登记文本"
	case "approve":
		label = "通过" + target
	case "reject":
		label = "拒绝" + target
	case "account":
		label = "关联租户登录账号"
		if method == http.MethodDelete {
			label = "解除租户登录账号关联"
		}
	case "members":
		label = "添加" + target + "成员"
	case ":uid":
		label = verb + target + "成员"
	case "settings":
		label = "保存台账设置"
		if method == http.MethodDelete {
			label = "恢复台账默认设置"
		}
	case "respond":
		label = "处理台账邀请"
	case "move":
		label = "移动房源所属台账"
	case "attachments":
		label = "上传租户附件"
	case "contracts":
		if len(parts) > 1 {
			label = "上传租赁合同"
		}
	case "agreements":
		label = "生成合同归档 PDF"
	case "photos":
		label = "上传" + target + "照片"
	case "meter-photos":
		if method == http.MethodPost {
			label = "上传抄表照片"
		}
	case "items":
		label = "添加房内物品"
	case "read":
		label = "标记站内消息已读"
	case "password":
		label = "修改账号密码"
	case "apikey":
		label = "重新生成 API 密钥"
	case "status":
		label = "修改用户启用状态"
	case "login":
		label = "登录账号"
	case "logout":
		label = "退出登录"
	case "register":
		label = "注册账号"
	case "upload":
		label = "上传备份"
	case "restore":
		label = "恢复备份"
	case "document-templates":
		label = "发布合同模板版本"
	case "plans":
		label = "保存周期任务计划"
	case "run":
		label = "检查并执行周期任务"
	case "subscriptions":
		label = "订阅设备推送"
		if method == http.MethodDelete {
			label = "取消设备推送订阅"
		}
	case "test":
		label = "发送设备推送测试"
	}
	return label, target
}

func displayAction(action string) string {
	if method, path, ok := genericActionRoute(action); ok {
		label, _ := describeMutation(method, path)
		return label
	}
	labels := map[string]string{
		"rental.tenant_account.bind": "关联租户登录账号", "rental.tenant_account.unbind": "解除租户登录账号关联",
		"login": "登录账号", "logout": "退出登录", "register": "注册账号",
		"wechat_login": "微信登录", "wechat_phone": "填写入住手机号",
		"backup_create": "创建数据库备份",
	}
	if label := labels[action]; label != "" {
		return label
	}
	return action
}

func genericActionRoute(action string) (string, string, bool) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		prefix := strings.ToLower(method) + "_api_"
		if strings.HasPrefix(action, prefix) {
			return method, "/api/" + strings.ReplaceAll(strings.TrimPrefix(action, prefix), "_", "/"), true
		}
	}
	return "", "", false
}

func mutationDetail(label string, id uint) string {
	if id > 0 {
		return label + "（对象编号 " + strconv.FormatUint(uint64(id), 10) + "）"
	}
	return label
}
