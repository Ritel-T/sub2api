package service

import "context"

// Legacy scope tests deliberately use authority without payer email bindings.
func withVerifiedSquarespaceReceiptClaim(ctx context.Context, userID, localOrderID int64, externalOrderID, quoteHash string) context.Context {
	return context.WithValue(ctx, squarespaceVerifiedReceiptClaimKey{}, squarespaceVerifiedReceiptClaimAuthority{userID: userID, localOrderID: localOrderID, externalOrderID: externalOrderID, quoteHash: quoteHash})
}
