function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character] || character)
}

function isSafeImageUrl(value: string): boolean {
  const trimmed = value.trim()
  if ((trimmed.startsWith('/') && !trimmed.startsWith('//')) || /^data:image\//i.test(trimmed)) {
    return true
  }
  try {
    const parsed = new URL(trimmed)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

export function injectBranding(html: string, config: { site_name?: string; site_logo?: string }): string {
  // This frontend ships the selected Ritel.SI identity; legacy public settings
  // still reach the app but must not replace the first-paint title or favicon.
  if (html.includes('<meta name="ritel-brand" content="fixed"')) return html
  let brandedHtml = html
  const siteName = config.site_name?.trim()
  if (siteName) {
    brandedHtml = brandedHtml.replace(
      /<title>[^<]*<\/title>/i,
      `<title>${escapeHtml(siteName)} - AI API Gateway</title>`,
    )
  }

  const siteLogo = config.site_logo?.trim()
  if (siteLogo && isSafeImageUrl(siteLogo)) {
    brandedHtml = brandedHtml.replace(
      /<link\s+rel=["']icon["'][^>]*>/i,
      `<link rel="icon" href="${escapeHtml(siteLogo)}" />`,
    )
  }
  return brandedHtml
}

