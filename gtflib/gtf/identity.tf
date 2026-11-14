;;;; gtf identity: keep tf's library helpers working with our `version' text.

/loaded gtf/identity.tf

;; /ver returns the TinyFugue release we are compatible with ("5.0 beta 8"),
;; which is what scripts feed to /vercmp.  (stdlib.tf's /ver parses a version
;; string that only real tf produces.)
/undef ver
/def -i ver = /result TFVERSION
