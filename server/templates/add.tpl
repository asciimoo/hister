{{define "main"}}
<div class="container">
    <form method="post">
        <input type="text" placeholder="URL..." name="url" class="full-width" /><br />
        <input type="text" placeholder="Title..." name="title" class="full-width" /><br />
        <textarea placeholder="Text..." name="text" class="full-width"></textarea>
        <input type="submit" value="Add" />
    </form>
</div>
{{end}}
