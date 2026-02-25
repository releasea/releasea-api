package models

type UserAuthPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type RefreshTokenPayload struct {
	RefreshToken string `json:"refreshToken"`
}

type PasswordResetConfirmPayload struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}
