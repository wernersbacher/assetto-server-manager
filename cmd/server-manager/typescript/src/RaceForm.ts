// RaceForm enhances the long race setup forms (custom race, championship event, race weekend session, quick race,
// championship and race weekend settings). The existing top level cards are kept as they are (so all the scripts
// that find fields inside them keep working) and are marked as sections. On top of that it adds
//  - a sticky section index (a horizontally scrolling strip, so it also works on phones),
//  - a sticky save bar that repeats the submit buttons of the form. The bar buttons just click the original
//    buttons, so name/value, ids and the scripts bound to them keep working,
//  - an "unsaved changes" indicator.
export class RaceForm {
    public constructor() {
        $("form.race-setup, form#championship-form, form#raceweekend-form").each((_, form) => this.build($(form)));
    }

    private build($form: JQuery) {
        const form = $form[0];
        const sections: { id: string, title: string, $card: JQuery }[] = [];

        $form.find(".card").each((_, card) => {
            const $card = $(card);
            const $header = $card.children(".card-header").first();

            // only top level cards (not the weather or entrant cards inside of other cards)
            if (!$header.length || $card.parentsUntil(form, ".card").length || $card.closest(".entrant, .template").length) {
                return;
            }

            const title = $header.find("strong").first().text().trim();

            if (!title || $card.is("[style*='display: none']")) {
                return;
            }

            const id = "form-section-" + sections.length;

            $card.addClass("form-section").attr("data-section", id);
            sections.push({id, title, $card});
        });

        // anchors: the card ids are already used for other things on some pages, so use an extra anchor element
        sections.forEach((s) => {
            s.$card.before($("<span>").addClass("form-section-anchor").attr("id", s.id));
        });

        this.buildIndex($form, sections);
        this.buildSaveBar($form);
    }

    private buildIndex($form: JQuery, sections: { id: string, title: string, $card: JQuery }[]) {
        if (sections.length < 3) {
            return;
        }

        const $index = $("<nav>").addClass("form-index").attr("aria-label", "Sections");
        const $list = $("<ul>");

        sections.forEach((s) => {
            $list.append($("<li>").append($("<a>").attr("href", "#" + s.id).text(s.title)));
        });

        $index.append($list);
        $form.prepend($index);

        const links = $list.find("a").toArray();
        const update = () => {
            let current = 0;

            sections.forEach((s, i) => {
                if (s.$card[0].getBoundingClientRect().top < 140) {
                    current = i;
                }
            });

            links.forEach((l, i) => {
                const active = i === current;
                $(l).toggleClass("active", active);

                // keep the active entry visible in the scrolling strip
                if (active && $index[0].scrollWidth > $index[0].clientWidth) {
                    const el = l.parentElement!;
                    const left = el.offsetLeft - ($index[0].clientWidth - el.offsetWidth) / 2;
                    $index[0].scrollTo({left: left});
                }
            });
        };

        $(window).on("scroll resize", update);
        update();
    }

    private buildSaveBar($form: JQuery) {
        const $buttons = $form.find("button[type='submit']");

        if (!$buttons.length) {
            return;
        }

        // the buttons of the last block that has submit buttons are the "real" actions of the form
        const form = $form[0];
        let top: HTMLElement | null = $buttons.last()[0];

        while (top && top.parentElement !== form) {
            top = top.parentElement;
        }

        const originals = $(top as HTMLElement).find("button[type='submit']").addBack("button[type='submit']").toArray() as HTMLButtonElement[];

        if (!originals.length) {
            return;
        }

        const $bar = $("<div>").addClass("save-bar race-save-bar");
        const $text = $("<span>").addClass("save-bar-text").text("No changes yet");
        $bar.append($text);

        const clones = originals.map((original) => {
            const $original = $(original);
            const $clone = $("<button>").attr("type", "button")
                .attr("class", $original.attr("class") || "btn btn-success")
                .removeClass("mt-5 float-right")
                .text($original.text().trim());

            if ($original.attr("title")) {
                $clone.attr("title", $original.attr("title")!);
            }

            $clone.on("click", () => original.click());
            $bar.append($clone);

            return $clone;
        });

        // the original buttons can be hidden by the form scripts (e.g. start race when a race is scheduled)
        const sync = () => {
            originals.forEach((original, i) => {
                clones[i].toggle($(original).css("display") !== "none");
            });
        };

        $form.append($bar);
        sync();

        let dirty = false;
        let submitted = false;

        $form.on("input change", (e: any) => {
            if (e.isTrigger || dirty || submitted) {
                return;
            }

            dirty = true;
            $text.text("Unsaved changes").addClass("is-dirty");
        });

        $form.on("input change click keyup", () => setTimeout(sync, 0));
        $form.on("submit", () => submitted = true);
    }
}
