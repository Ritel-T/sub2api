export default {
  title: 'QQ 通知', description: '绑定 QQ，接收余额不足和套餐到期提醒。',
  unavailable: 'QQ 通知暂未开放，现有邮箱设置继续生效。',
  bound: '已绑定', unbound: '未绑定', bindHint: '生成绑定码，私聊发送给机器人，再回到这里确认 QQ。',
  createCode: '生成绑定码', regenerate: '重新生成绑定码', addFriend: '添加 QQ {qq} 为好友。',
  sendCommand: '在与机器人的私聊中发送以下完整消息：', copy: '复制消息', copied: '消息已复制',
  copyFailed: '复制失败，请选中消息手动复制。', codeHidden: '已发送绑定码？点击下方查看回复；未发送则重新生成绑定码。',
  expires: '有效期至 {time}，重新生成后旧码失效。', waiting: '发送后点击「查看回复」，核对并确认你的 QQ。',
  candidate: 'QQ {qq} 请求绑定。请确认这是你自己的账号。', confirm: '确认绑定此 QQ', refresh: '查看回复',
  expired: '绑定码已过期，请重新生成。', retry: '重试', unlink: '解绑 QQ',
  unlinkMessage: '确认解绑此 QQ？后续通知将改用邮箱，QQ 内的账号查询也将停止。',
  channel: '通知渠道', email: '邮箱', both: '邮箱和 QQ',
  channelHint: '绑定后可选择 QQ。账号找回和安全邮件仍使用邮箱。',
  events: '通知内容', balanceLow: '余额不足', subscriptionExpiry: '套餐到期',
  balanceHint: '余额不足提醒同时遵循余额通知的开关和阈值设置。',
  commands: '私聊机器人发送「/sub 余额」「/sub 套餐」或「/sub 今日用量」，即可查询当前账号。'
}