/** Only the backend-validated PNG/JPEG mini-program image is rendered as-is.
 * Re-encoding it as a normal QR would destroy the official mini-program code. */
export function isMiniProgramImage(paymentType: string, source: string): boolean {
  return paymentType === 'gelv' && source.length <= 1048576
    && /^data:image\/(?:png|jpeg);base64,[A-Za-z0-9+/]+={0,2}$/.test(source)
}
