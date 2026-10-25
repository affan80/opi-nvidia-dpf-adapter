NVIDIA enablement contract — choice

Date: 2026-10-22
Skill: /Superpowers (brainstorming then start working on this)

Brainstorm
1. Existing-object annotation lifecycle.opi.nvidia.com/vendor=nvidia
   - Passes the installed OPI CRD (spec may only contain logLevel)
   - Already used by the LCM as a fallback
2. Versioned upstream schema change: add spec.vendor enum
   - This adapter does not own config.openshift.io
   - On dh2, spec.vendor is a BadRequest (strict unknown field)
   - Shipping a patched CRD here would be a fork

Choice
Use the annotation. Reject unknown vendors in the LCM. Keep reading
spec.vendor first so a future upstream field wins without a code change.

Done when
config/samples/dpuoperatorconfig-nvidia.yaml applied on dh2.
CRD validation passed. LCM log vendor=nvidia.

Reports
- issue_01_spec_vendor_rejected.md
- issue_02_unknown_vendor_not_rejected.md
- issue_03_no_config_object.md
- issue_04_running_image_old_window.md
