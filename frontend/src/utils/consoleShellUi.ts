export function consoleShellCopy(locale: string) {
  if (locale.startsWith('zh')) {
    const hk = /Hant|HK|TW/i.test(locale)
    return {
      available: hk ? '可用餘額' : '可用余额',
      frozen: hk ? '凍結金額' : '冻结金额',
      total: hk ? '總額' : '总额',
      skip: hk ? '跳至主要內容' : '跳至主要内容',
      unread: (count: number) => hk ? `${count} 項待處理` : `${count} 项待处理`,
    }
  }
  return {
    available: 'Available balance', frozen: 'Frozen balance', total: 'Total balance', skip: 'Skip to content',
    unread: (count: number) => `${count} pending`,
  }
}

export function formatNavBadge(count: number): string {
  return count > 99 ? '99+' : String(count)
}
