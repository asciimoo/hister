{{define "main"}}
<div class="container full-width">
    <form method="post">
        <h2>Skip Rules</h2>
        <textarea placeholder="Text..." name="skip" class="full-width" >{{ Join .Config.Rules.Skip.ReStrs "\n" }}</textarea>
        <h2>Priority Rules</h2>
        <textarea placeholder="Text..." name="priority" class="full-width" >{{ Join .Config.Rules.Priority.ReStrs "\n" }}</textarea>
        <input type="submit" value="Save" class="mt-1" />
    </form>
</div>
{{end}}
