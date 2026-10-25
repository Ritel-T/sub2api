import { describe, expect, it } from 'vitest'
import { shouldRecommendCaptchaBrowser } from '../captchaBrowser'

const android = 'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Mobile Safari/537.36'
const ios = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko)'
const desktop = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36'

describe('CAPTCHA browser recommendation', () => {
  it.each([
    ['Chrome desktop', desktop], ['Chrome Android', android],
    ['Chrome iOS', ios + ' CriOS/135.0.0.0 Mobile/15E148 Safari/604.1'],
    ['Edge desktop', desktop + ' Edg/135.0.0.0'],
    ['Edge Android', android + ' EdgA/135.0.0.0'],
    ['Edge iOS', ios + ' EdgiOS/135.0.0.0 Mobile/15E148 Safari/605.1.15'],
    ['Firefox desktop', 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:137.0) Gecko/20100101 Firefox/137.0'],
    ['Firefox iOS', ios + ' FxiOS/137.0 Mobile/15E148 Safari/605.1.15'],
    ['Safari iOS', ios + ' Version/18.3 Mobile/15E148 Safari/604.1'],
    ['Safari macOS', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.3 Safari/605.1.15']
  ])('does not warn in %s', (_name, ua) => expect(shouldRecommendCaptchaBrowser(ua)).toBe(false))

  it.each([
    ['QQ Android with Chrome tokens', android + ' QQ/9.1.65.1 V1_AND_SQ_9.1.65'],
    ['QQ iOS with Safari tokens', ios + ' Version/18.3 Mobile/15E148 Safari/604.1 QQ/9.1.65'],
    ['QQ Browser', android + ' MQQBrowser/15.0'],
    ['reported Windows QQ EX', 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) QQEX/9.9.35-52892 Chrome/144.0.7559.60 Safari/537.36 OS/win32,x64 QQAppId/537382819 QQEXBrowser/1.0.0.0 QQWebContentView/1.0.0.0'],
    ...['QQEX/9.9.35', 'QQEXBrowser/1.0.0.0', 'QQWebContentView/1.0.0.0', 'QQAppId/537382819'].map(marker => [marker, desktop + ' ' + marker]),
    ['WeChat', android + ' MicroMessenger/8.0.60'],
    ['Android WebView', android.replace('Android 15;', 'Android 15; wv;')],
    ['iOS WebView', ios + ' Mobile/15E148'],
    ['DingTalk', android + ' DingTalk/7.0'],
    ['Alipay', android + ' AlipayClient/10.6'],
    ['Quark', android + ' Quark/7.0'],
    ['UC', android + ' UCBrowser/17.0'],
    ['Samsung', android + ' SamsungBrowser/28.0'],
    ['Opera', desktop + ' OPR/118.0'],
    ['unknown', 'ExampleBrowser/1.0'], ['empty', '']
  ])('warns in %s', (_name, ua) => expect(shouldRecommendCaptchaBrowser(ua)).toBe(true))
})
