export const oauthErrors: Record<string, string> = {
  invalid_state: "登录请求已过期或浏览器状态不匹配，请重新开始。",
  access_denied: "你取消了第三方授权，可以重新选择登录方式。",
  provider_error: "第三方登录暂时失败，请重试或使用其他登录方式。",
  provider_unavailable: "此部署尚未配置该登录方式。",
  verified_email_required: "请先在 Google / GitHub 验证邮箱，再重新登录。",
  email_conflict: "这个邮箱已有 Lester 账号。请先用原来的方式登录，再到个人资料绑定此账号；忘记密码时可通过邮件找回。",
  identity_conflict: "此第三方账号已绑定其他账号，或你已绑定该服务的另一个账号。",
  registration_closed: "此部署已关闭新账号注册，请联系管理员。",
  account_unavailable: "账号暂时无法登录，请联系管理员。",
  session_expired: "原登录已失效，请重新登录后再绑定。",
};
