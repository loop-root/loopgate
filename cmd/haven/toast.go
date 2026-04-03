package main

// EmitToast sends a toast notification to the Haven frontend.
// Variants: "info", "success", "warning".
func (app *HavenApp) EmitToast(title, message, variant string) {
	if app.emitter == nil {
		return
	}
	app.emitter.Emit("haven:toast", map[string]interface{}{
		"title":   title,
		"message": message,
		"variant": variant,
	})
}
