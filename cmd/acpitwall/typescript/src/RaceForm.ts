// RaceForm enhances the long race setup forms (custom race, championship event, race weekend session, quick race,
// championship and race weekend settings). The existing top level cards are kept as they are (so all the scripts
// that find fields inside them keep working) and are marked as sections. On top of that it adds
//  - a sticky section index (a horizontally scrolling strip, so it also works on phones) with status markers
//    (error / edited / complete) per section,
//  - a sticky save bar that repeats the submit buttons of the form. The bar buttons just click the original
//    buttons, so name/value, ids and the scripts bound to them keep working,
//  - an "unsaved changes" indicator and a "Discard changes" button,
//  - collapsible advanced sections (see COLLAPSIBLE) that show a one line summary of their values while collapsed,
//  - an error summary for fields the browser refuses to submit, which links to (and opens) the affected sections.
// Collapsing only hides the card body with CSS: the fields stay in the DOM, so scripts and submission still see them.

type SummaryField = { name: string, label: string };
type Section = { id: string, title: string, $card: JQuery, baseline: string, collapsible: boolean };

// The advanced sections that most users leave at their defaults, with the fields shown in the collapsed summary.
const COLLAPSIBLE: { [title: string]: SummaryField[] } = {
    "Assists": [
        {name: "ABSAllowed", label: "ABS"}, {name: "TractionControlAllowed", label: "TC"},
        {name: "StabilityControlAllowed", label: "Stability"}, {name: "AutoClutchAllowed", label: "Autoclutch"},
        {name: "TyreBlanketsAllowed", label: "Tyre blankets"},
    ],
    "Realism": [
        {name: "FuelRate", label: "Fuel"}, {name: "DamageMultiplier", label: "Damage"},
        {name: "TyreWearRate", label: "Tyre wear"}, {name: "ForceVirtualMirror", label: "Virtual mirror"},
        {name: "SurfacePreset", label: "Surface"}, {name: "SessionStart", label: "Grip at start"},
    ],
    "Rules": [
        {name: "MaxBallastKilograms", label: "Max ballast"}, {name: "AllowedTyresOut", label: "Tyres out"},
        {name: "MaxContactsPerKilometer", label: "Contacts/km"}, {name: "StartRule", label: "Start rule"},
        {name: "LoopMode", label: "Loop"}, {name: "ResultScreenTime", label: "Result screen"},
        {name: "DisableDRSZones", label: "No DRS zones"},
    ],
    "Server Options": [
        {name: "OverridePassword", label: "Replace password"}, {name: "ForceStopTime", label: "Force stop"},
        {name: "ForceStopWithDrivers", label: "Stop with drivers"},
    ],
};

export class RaceForm {
    public constructor() {
        $("form.race-setup, form#championship-form, form#raceweekend-form").each((_, form) => this.build($(form)));
    }

    private build($form: JQuery) {
        const form = $form[0];
        const sections: Section[] = [];

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
            sections.push({
                id, title, $card, baseline: "",
                collapsible: !!COLLAPSIBLE[title] && $card.children(".card-body").length > 0,
            });
        });

        // anchors: the card ids are already used for other things on some pages, so use an extra anchor element
        sections.forEach((s) => {
            s.$card.before($("<span>").addClass("form-section-anchor").attr("id", s.id));
            s.baseline = RaceForm.signature(s.$card);
        });

        this.buildCollapsible($form, sections, "sm-form-sections:" + RaceForm.formType($form));
        const refresh = this.buildIndex($form, sections);
        this.buildErrorSummary($form, sections, refresh);
        this.buildServerErrors($form, sections, refresh);
        this.buildSaveBar($form);

        // The page scripts still adjust some fields while the page loads (pickers, defaults), so the values the
        // "edited" markers compare with are taken once the page has settled, unless the user already changed something.
        let touched = false;

        $form.on("input change", (e: any) => touched = touched || !e.isTrigger);
        const rebase = () => setTimeout(() => {
            if (!touched) {
                sections.forEach((s) => s.baseline = RaceForm.signature(s.$card));
                $form.trigger("change");
            }
        }, 400);

        if (document.readyState === "complete") {
            rebase();
        } else {
            $(window).on("load", rebase);
        }
    }

    // formType names the kind of form, so the collapsed state is remembered separately for e.g. custom races and
    // championship events.
    private static formType($form: JQuery): string {
        const action = ($form.attr("action") || "") + " " + ($form.attr("id") || "");

        if (/\/championship\/.*\/event/.test(action)) {
            return "championship-event";
        } else if (/race-weekend\/.*\/session/.test(action)) {
            return "raceweekend-session";
        } else if (/\/quick/.test(action)) {
            return "quick-race";
        } else if (/\/custom/.test(action)) {
            return "custom-race";
        }

        return $form.attr("id") || "race-form";
    }

    // signature serialises the values of all fields in an element, to find out whether anything was edited.
    private static signature($el: JQuery): string {
        return $el.find(":input").toArray().map((el: any) => {
            if (!el.name) {
                return "";
            }

            if (el.type === "checkbox" || el.type === "radio") {
                return el.name + "=" + el.checked;
            }

            return el.name + "=" + $(el).val();
        }).join("&");
    }

    private static load(key: string): { [title: string]: boolean } {
        try {
            return JSON.parse(localStorage.getItem(key) || "{}") || {};
        } catch (e) {
            return {};
        }
    }

    private static save(key: string, state: { [title: string]: boolean }) {
        try {
            localStorage.setItem(key, JSON.stringify(state));
        } catch (e) {
            // storage can be unavailable (private mode): the sections then just start collapsed again
        }
    }

    private static summarise($card: JQuery, fields: SummaryField[]): string {
        const parts: string[] = [];

        fields.forEach((f) => {
            const $field = $card.find("[name='" + f.name + "']").not("[type='hidden']").first();

            if (!$field.length) {
                return;
            }

            const el = $field[0] as any;

            if (el.type === "checkbox") {
                parts.push(f.label + (el.checked ? " on" : " off"));
            } else if (el.tagName === "SELECT") {
                const selected = Array.from(el.selectedOptions as HTMLOptionElement[]).map((o) => o.text.trim());
                const text = el.multiple ? (selected.length ? selected.length + " selected" : "none selected") : selected.join(", ");

                if (text) {
                    parts.push(f.label + ": " + text);
                }
            } else if (el.value !== "") {
                parts.push(f.label + ": " + el.value);
            }
        });

        return parts.join(", ");
    }

    private static setOpen(s: Section, open: boolean) {
        if (!s.collapsible) {
            return;
        }

        const wasCollapsed = s.$card.hasClass("is-collapsed");

        s.$card.toggleClass("is-collapsed", !open);
        s.$card.find(".form-section-toggle").attr("aria-expanded", String(open))
            .find(".form-section-toggle-text").text(open ? "Hide" : "Show");

        if (open && wasCollapsed) {
            // widgets that measure their layout (select pickers, charts) get a chance to update
            $(window).trigger("resize");
        }
    }

    private buildCollapsible($form: JQuery, sections: Section[], storageKey: string) {
        const stored = RaceForm.load(storageKey);

        sections.filter((s) => s.collapsible).forEach((s) => {
            const $header = s.$card.children(".card-header").first();
            const bodyId = s.id + "-body";

            s.$card.children(".card-body").first().attr("id", bodyId);

            const $summary = $("<div>").addClass("form-section-summary");
            const $edited = $("<span>").addClass("badge badge-secondary form-section-edited").text("edited").hide();
            const $toggle = $("<button>").attr({type: "button", "aria-expanded": "true", "aria-controls": bodyId})
                .addClass("form-section-toggle btn btn-sm btn-outline")
                .attr("aria-label", "Show or hide " + s.title)
                .append($("<span>").addClass("form-section-toggle-text").text("Hide"));
            const $strong = $header.children("strong").first();
            const $heading = $("<div>").addClass("form-section-heading").append($("<div>").append($strong).append($edited)).append($summary);

            $header.addClass("form-section-header").empty().append($heading).append($toggle);

            const update = () => {
                $summary.text(RaceForm.summarise(s.$card, COLLAPSIBLE[s.title]));
                $edited.toggle(RaceForm.signature(s.$card) !== s.baseline);
            };

            update();
            $form.on("input change", () => setTimeout(update, 0));

            const toggle = () => {
                const open = s.$card.hasClass("is-collapsed");

                RaceForm.setOpen(s, open);
                stored[s.title] = open;
                RaceForm.save(storageKey, stored);
            };

            $toggle.on("click", toggle);
            $header.on("click", (e) => {
                if (!$(e.target).closest("button, a, input, select, label").length) {
                    toggle();
                }
            });

            RaceForm.setOpen(s, stored[s.title] === true);
        });
    }

    private buildIndex($form: JQuery, sections: Section[]): () => void {
        if (sections.length < 3) {
            return () => undefined;
        }

        const $index = $("<nav>").addClass("form-index").attr("aria-label", "Sections");
        const $list = $("<ul>");
        const markers: JQuery[] = [];

        sections.forEach((s) => {
            const $marker = $("<span>").addClass("form-index-mark");
            markers.push($marker);
            $list.append($("<li>").append($("<a>").attr("href", "#" + s.id).text(s.title).prepend($marker)));
        });

        $index.append($list);
        $form.prepend($index);

        // status per section: has an error, was edited since the page was loaded, or is complete (has required
        // fields which are all filled in)
        const refresh = () => {
            sections.forEach((s, i) => {
                let status = "";
                let text = "";
                const $required = s.$card.find(":input[required]").filter((_, el: any) => !$(el).closest(".template").length);

                if (s.$card.hasClass("has-error")) {
                    status = "error";
                    text = "has errors";
                } else if (RaceForm.signature(s.$card) !== s.baseline) {
                    status = "edited";
                    text = "edited";
                } else if ($required.length && $required.toArray().every((el: any) => el.checkValidity())) {
                    status = "complete";
                    text = "complete";
                }

                markers[i].attr("data-status", status).empty();

                if (text) {
                    markers[i].append($("<span>").addClass("sr-only").text(text + ": "));
                }
            });
        };

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

        let timer: any;

        $form.on("input change", () => {
            clearTimeout(timer);
            timer = setTimeout(refresh, 150);
        });
        refresh();

        return refresh;
    }

    private buildErrorSummary($form: JQuery, sections: Section[], refresh: () => void) {
        const form = $form[0] as HTMLFormElement;
        let $summary: JQuery | null = null;
        let batchOpen = false;
        const errors: { el: HTMLElement, $li: JQuery }[] = [];

        const sectionOf = (el: HTMLElement) => sections.filter((s) => s.$card[0].contains(el))[0];

        const labelOf = (el: HTMLElement) => {
            const id = el.getAttribute("id");
            let text = id ? $("label[for='" + id + "']").first().text().trim() : "";

            if (!text) {
                text = $(el).closest(".form-group, td").find("label").first().text().trim();
            }

            return text || el.getAttribute("name") || "Field";
        };

        const clear = () => {
            errors.splice(0).forEach((e) => {
                const s = sectionOf(e.el);

                $(e.el).removeAttr("aria-invalid");

                if (s) {
                    s.$card.removeClass("has-error");
                }
            });

            if ($summary) {
                $summary.remove();
                $summary = null;
            }
        };

        const show = () => {
            $summary = $("<div>").addClass("form-error-summary alert alert-danger").attr({role: "alert", tabindex: "-1"})
                .append($("<strong>").text("Please fix these fields before saving:"));
            const $list = $("<ul>");

            errors.forEach((e) => $list.append(e.$li));
            $summary.append($list);

            const $index = $form.children(".form-index");

            if ($index.length) {
                $index.after($summary);
            } else {
                $form.prepend($summary);
            }

            refresh();
            $summary[0].scrollIntoView({block: "start"});
            $summary.trigger("focus");
        };

        // invalid events don't bubble, so listen in the capture phase. The browser can only focus and report a
        // field that is visible, so collapsed sections are opened synchronously here.
        // Page scripts also call checkValidity(), which fires invalid events too: only a submit attempt counts.
        let attempt = false;

        form.addEventListener("click", (e) => {
            if ($(e.target as HTMLElement).closest("button[type=submit], input[type=submit]").length) {
                attempt = true;
                setTimeout(() => attempt = false, 0);
            }
        }, true);

        form.addEventListener("invalid", (e) => {
            if (!attempt) {
                return;
            }

            const el = e.target as HTMLElement;
            const s = sectionOf(el);

            if (!batchOpen) {
                batchOpen = true;
                clear();
                setTimeout(() => {
                    batchOpen = false;
                    show();
                }, 0);
            }

            if (s) {
                RaceForm.setOpen(s, true);
                s.$card.addClass("has-error");
            }

            $(el).attr("aria-invalid", "true");

            const message = (el as HTMLInputElement).validationMessage;
            const $a = $("<a>").attr("href", "#").text((s ? s.title + ": " : "") + labelOf(el) + (message ? " - " + message : ""));

            $a.on("click", (ev) => {
                ev.preventDefault();

                if (s) {
                    RaceForm.setOpen(s, true);
                }

                el.scrollIntoView({block: "center"});
                el.focus({preventScroll: true});
            });

            errors.push({el, $li: $("<li>").append($a)});
        }, true);

        // fixed fields drop out of the summary
        $form.on("input change", (e) => {
            errors.filter((x) => x.el === e.target && (x.el as HTMLInputElement).checkValidity()).forEach((x) => {
                x.$li.remove();
                errors.splice(errors.indexOf(x), 1);
                $(x.el).removeAttr("aria-invalid");
                const s = sectionOf(x.el);

                if (s && !errors.some((o) => sectionOf(o.el) === s)) {
                    s.$card.removeClass("has-error");
                }
            });

            if ($summary && !errors.length) {
                clear();
            }

            refresh();
        });
    }

    // buildServerErrors turns the error list the server rendered (the form was submitted with invalid values) into the
    // same kind of summary: the entries link to the fields, and the fields and sections are marked.
    private buildServerErrors($form: JQuery, sections: Section[], refresh: () => void) {
        const $summary = $form.children(".server-form-errors").first();

        if (!$summary.length) {
            return;
        }

        const $index = $form.children(".form-index");

        if ($index.length) {
            $index.after($summary);
        }

        $summary.addClass("form-error-summary");

        $summary.find("li[data-field]").each((_, li) => {
            const $li = $(li);
            const name = $li.attr("data-field") || "";
            const el = $form.find("[name='" + name.replace(/'/g, "") + "']").not("[type='hidden']").first()[0] as HTMLElement | undefined;

            if (!el) {
                return;
            }

            const s = sections.filter((x) => x.$card[0].contains(el))[0];
            const $a = $("<a>").attr("href", "#").text((s ? s.title + ": " : "") + $li.text());

            $(el).attr("aria-invalid", "true");

            if (s) {
                RaceForm.setOpen(s, true);
                s.$card.addClass("has-error");
            }

            $a.on("click", (ev) => {
                ev.preventDefault();

                if (s) {
                    RaceForm.setOpen(s, true);
                }

                el.scrollIntoView({block: "center"});
                el.focus({preventScroll: true});
            });

            // a changed field is no longer reported
            $(el).one("input change", () => {
                $li.remove();
                $(el).removeAttr("aria-invalid");

                if (s && !$form.find("[aria-invalid='true']").filter((_i, o) => s.$card[0].contains(o)).length) {
                    s.$card.removeClass("has-error");
                }

                if (!$summary.find("li").length) {
                    $summary.remove();
                }

                refresh();
            });

            $li.empty().append($a);
        });

        refresh();
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

        let dirty = false;
        let submitted = false;

        // Discard throws the edits away by loading the page again; the shared dialog asks first when there are edits
        const $discard = $("<button>").attr("type", "button").addClass("btn btn-outline race-discard").text("Discard changes");
        const reload = () => {
            window.onbeforeunload = null;
            window.location.assign(window.location.href.split("#")[0]);
        };

        $discard.on("click", () => {
            const $modal = $("#delete-confirm-modal");

            if (!dirty || !$modal.length) {
                reload();
                return;
            }

            $("#delete-confirm-title").text("Discard your changes?");
            $("#delete-confirm-detail").text("The form is reset to the last saved values.");
            $("#delete-confirm-ok").text("Discard").one("click.discard", reload);
            $modal.one("hidden.bs.modal", () => $("#delete-confirm-ok").off("click.discard"));
            $modal.modal("show");
        });

        $bar.append($discard);

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
