package fsmsolana

const (
	stageBeforeSending = "before_sending"
	stageSending       = "sending"
	stageAfterSending  = "after_sending"
)

const (
	stepValidateRequest                = "validate_request"
	stepSendSOLForFees                 = "send_sol_for_fees"
	stepWaitingSendSOLForFees          = "waiting_send_sol_for_fees_confirmations"
	stepSending                        = "sending"
	stepWaitingForTheFirstConfirmation = "waiting_for_the_first_confirmation"
	stepWaitingConfirmations           = "waiting_confirmations"
	stepSendSuccessEvent               = "send_success_event"
)

var exitOnFailedSteps = map[string][]string{
	stageBeforeSending: {stepValidateRequest, stepSendSOLForFees},
	stageSending:       {stepSending},
}

const (
	SendSOLForFees       = "send_sol_for_fees"
	SendSOLForFeesTxHash = "send_sol_for_fees_tx_hash"
	StateKeyMemo         = "memo"
	StateKeyReference    = "reference"
)
