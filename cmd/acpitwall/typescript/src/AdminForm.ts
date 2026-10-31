// AdminForm restyles the long option forms that are generated server side by formulate (server options and plugin
// options). The generated markup is a single <fieldset> of rows, where every section starts with a row containing
// an <h3> (preceded by an <hr>). This turns each section into a card and fills the optional on-page section index
// (#options-index) with links to the cards. Field names, ids and the rows themselves are left untouched.
export class AdminForm {
    public constructor() {
        $("form.admin-form").each((_, form) => this.build($(form)));
    }

    private build($form: JQuery) {
        const $fieldset = $form.find("fieldset").first();

        if (!$fieldset.length) {
            return;
        }

        const sections: { id: string, title: string, $card: JQuery }[] = [];
        let $body: JQuery | null = null;

        $fieldset.children().toArray().forEach((row) => {
            const $row = $(row);
            const $heading = $row.find("> div > h3").first();

            if ($heading.length) {
                const id = "section-" + sections.length;
                const title = $heading.text().trim();
                const $card = $("<section>").addClass("card admin-card options-section").attr("id", id);

                $card.append($("<div>").addClass("card-header").append($("<h2>").text(title)));
                $body = $("<div>").addClass("card-body");
                $card.append($body);

                $row.find("> div > hr").remove();
                const $col = $heading.parent();
                $heading.remove();

                if (!$col.children().length && !$col.text().trim()) {
                    $col.remove();
                }

                sections.push({id, title, $card});
                $fieldset.append($card);
            }

            if ($body) {
                $body.append($row);
            }
        });

        const $index = $form.find("#options-index");

        if (!$index.length || sections.length < 2) {
            return;
        }

        const $list = $index.find("ul");

        sections.forEach((s) => {
            $list.append($("<li>").append($("<a>").attr("href", "#" + s.id).text(s.title)));
        });

        $index.prop("hidden", false);

        // highlight the section that is currently in view
        const links = $list.find("a").toArray();
        const update = () => {
            let current = 0;

            sections.forEach((s, i) => {
                if (s.$card[0].getBoundingClientRect().top < 140) {
                    current = i;
                }
            });

            links.forEach((l, i) => $(l).toggleClass("active", i === current));
        };

        $(window).on("scroll resize", update);
        update();
    }
}
