// Package paneloffline validates independently persisted panel shutdown durations.
package paneloffline

import (
  "errors"
  "time"
)
const MaxMinutes=525600
// Duration bounds an enabled schedule to a positive integer within one year.
func Duration(enabled bool,amount int,unit string)(time.Duration,error){
 if !enabled{return 0,nil}
 factor:=0
 switch unit{case "minute":factor=1;case "hour":factor=60;case "day":factor=1440
 default:return 0,errors.New("invalid offline time unit")}
 if amount<1 || amount>MaxMinutes/factor{
  return 0,errors.New("offline time must be between 1 and 365 days")}
 return time.Duration(amount*factor)*time.Minute,nil
}
