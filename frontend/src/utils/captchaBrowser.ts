// User-agent detection is advisory: embedded browsers can include Chrome/Safari tokens.
// Check known embedded/alternative browsers before the four recommended families.
export function shouldRecommendCaptchaBrowser(userAgent: string): boolean {
  if (!userAgent.trim()) return true
  const embeddedOrAlternative = /MicroMessenger|\bQQ\/|MQQBrowser|QQBrowser|QQEX(?:Browser)?\/|QQWebContentView\/|QQAppId\/|TIM\/|;\s*wv\b|\bWebView\b|AlipayClient|DingTalk|Weibo|BytedanceWebview|ToutiaoMicroApp|aweme|baiduboxapp|baidubrowser|UCBrowser|UBrowser|Quark|SamsungBrowser|HuaweiBrowser|MiuiBrowser|VivoBrowser|HeyTapBrowser|OPR\/|Opera|YaBrowser|DuckDuckGo|HeadlessChrome/i
  if (embeddedOrAlternative.test(userAgent)) return true
  if (/Edg(?:A|iOS)?\/|Firefox\/|FxiOS\/|Chrome\/|CriOS\//i.test(userAgent)) return false
  return !(/Version\/[\d.]+/i.test(userAgent) && /Safari\//i.test(userAgent))
}
