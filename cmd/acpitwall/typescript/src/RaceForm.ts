// RaceForm enhances the long race setup forms (custom race, championship event, race weekend session, quick race,
// championship and race weekend settings). The existing top level cards are kept as they are (so all the scripts
// that find fields inside them keep working) and are marked as sections. On top of that it adds
//  - on forms with Bootstrap tabs (the championship form) no section index: the tabs are the navigation, they get the
//    status markers instead, and the tab with an invalid field or a server error is opened,
//  - a sticky section index (a horizontally scrolling strip, so it also works on phones) with status markers
//    (error / edited / complete) per section,
//  - a sticky save bar that repeats the submit buttons of the form. The bar buttons just click the original
//    buttons, so name/value, ids and the scripts bound to them keep working,
//  - an "unsaved changes" indicator and a "Discard changes" button,
//  - collapsible advanced sections (see COLLAPSIBLE) that show a one line summary of their values while collapsed,
//  - an error summary for fields the browser refuses to submit, which links to (and opens) the affected sections.
// Collapsing only hides the card body with CSS: the fields stay in the DOM, so scripts and submission still see them.
import {t} from "./i18n";

type SummaryField = { name: string, label: string };
type Section = { id: string, title: string, $card: JQuery, baseline: string, collapsible: boolean, group: string };

// The advanced sections that most users leave at their defaults, with the fields shown in the collapsed summary.
// The group names are only ids (also used to remember the collapsed state); a card belongs to a group when it
// contains most of the group's fields, because the card titles are translated.
let collapsibleGroups: { [group: string]: SummaryField[] } | null = null;

function collapsible(): { [group: string]: SummaryField[] } {
    if (!collapsibleGroups) {
        collapsibleGroups = {
            "Assists": [
                {name: "ABSAllowed", label: t("race_form.field.abs")}, {name: "TractionControlAllowed", label: t("race_form.field.tc")},
                {name: "StabilityControlAllowed", label: t("race_form.field.stability")}, {name: "AutoClutchAllowed", label: t("race_form.field.autoclutch")},
                {name: "TyreBlanketsAllowed", label: t("race_form.field.tyre_blankets")},
            ],
            "Realism": [
                {name: "FuelRate", label: t("race_form.field.fuel")}, {name: "DamageMultiplier", label: t("race_form.field.damage")},
                {name: "TyreWearRate", label: t("race_form.field.tyre_wear")}, {name: "ForceVirtualMirror", label: t("race_form.field.virtual_mirror")},
                {name: "SurfacePreset", label: t("race_form.field.surface")}, {name: "SessionStart", label: t("race_form.field.grip_at_start")},
            ],
            "Rules": [
                {name: "MaxBallastKilograms", label: t("race_form.field.max_ballast")}, {name: "AllowedTyresOut", label: t("race_form.field.tyres_out")},
                {name: "MaxContactsPerKilometer", label: t("race_form.field.contacts_per_km")}, {name: "StartRule", label: t("race_form.field.start_rule")},
                {name: "LoopMode", label: t("race_form.field.loop")}, {name: "ResultScreenTime", label: t("race_form.field.result_screen")},
                {name: "DisableDRSZones", label: t("race_form.field.no_drs_zones")},
            ],
            "Server Options": [
                {name: "OverridePassword", label: t("race_form.field.replace_password")}, {name: "ForceStopTime", label: t("race_form.field.force_stop")},
                {name: "ForceStopWithDrivers", label: t("race_form.field.stop_with_drivers")},
            ],
        };
    }

    return collapsibleGroups;
}

// collapsibleGroup returns the group of the card, or "" if it is not one of the collapsible sections.
function collapsibleGroup($card: JQuery): string {
    const groups = collapsible();

    for (const group in groups) {
        const fields = groups[group];
        const found = fields.filter((f) => $card.find("[name='" + f.name + "']").length > 0).length;

        if (found * 2 > fields.length) {
            return group;
        }
    }

    return "";
}

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
            const group = collapsibleGroup($card);

            $card.addClass("form-section").attr("data-section", id);
            sections.push({
                id, title, $card, baseline: "", group,
                collapsible: !!group && $card.children(".card-body").length > 0,
            });
        });

        // anchors: the card ids are already used for other things on some pages, so use an extra anchor element
        sections.forEach((s) => {
            s.$card.before($("<span>").addClass("form-section-anchor").attr("id", s.id));
            s.baseline = RaceForm.signature(s.$card);
        });

        this.buildCollapsible($form, sections, "sm-form-sections:" + RaceForm.formType($form));
        const $tabs = $form.find("ul.nav-tabs").first();
        const tabs = $tabs.length && $form.find(".tab-content").length ? this.buildTabs($form, $tabs, sections) : null;
        const refresh = tabs ? tabs.refresh : this.buildIndex($form, sections);
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

                if (tabs) {
                    tabs.rebase();
                }

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
                parts.push(el.checked ? t("race_form.summary.on", {label: f.label}) : t("race_form.summary.off", {label: f.label}));
            } else if (el.tagName === "SELECT") {
                const selected = Array.from(el.selectedOptions as HTMLOptionElement[]).map((o) => o.text.trim());
                const text = el.multiple ? (selected.length ? t("race_form.summary.selected", {count: selected.length}) : t("race_form.summary.none_selected")) : selected.join(", ");

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
            .find(".form-section-toggle-text").text(open ? t("race_form.hide") : t("race_form.show"));

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
            const $edited = $("<span>").addClass("badge badge-secondary form-section-edited").text(t("race_form.edited")).hide();
            const $toggle = $("<button>").attr({type: "button", "aria-expanded": "true", "aria-controls": bodyId})
                .addClass("form-section-toggle btn btn-sm btn-outline")
                .attr("aria-label", t("race_form.toggle_label", {section: s.title}))
                .append($("<span>").addClass("form-section-toggle-text").text(t("race_form.hide")));
            const $strong = $header.children("strong").first();
            const $heading = $("<div>").addClass("form-section-heading").append($("<div>").append($strong).append($edited)).append($summary);

            $header.addClass("form-section-header").empty().append($heading).append($toggle);

            const update = () => {
                $summary.text(RaceForm.summarise(s.$card, collapsible()[s.group]));
                $edited.toggle(RaceForm.signature(s.$card) !== s.baseline);
            };

            update();
            $form.on("input change", () => setTimeout(update, 0));

            const toggle = () => {
                const open = s.$card.hasClass("is-collapsed");

                RaceForm.setOpen(s, open);
                stored[s.group] = open;
                RaceForm.save(storageKey, stored);
            };

            $toggle.on("click", toggle);
            $header.on("click", (e) => {
                if (!$(e.target).closest("button, a, input, select, label").length) {
                    toggle();
                }
            });

            RaceForm.setOpen(s, stored[s.group] === true);
        });
    }

    // showTabOf opens the tab pane that holds the element (no-op without tabs). Panes switch synchronously (no fade),
    // so the element is displayed again right away and the browser can focus it.
    private static showTabOf(el: HTMLElement) {
        const $pane = $(el).closest(".tab-pane");

        if ($pane.length && !$pane.hasClass("active") && $pane.attr("id")) {
            $("a[data-toggle=tab][href=\"#" + $pane.attr("id") + "\"]").first().tab("show");
        }
    }

    // buildTabs adds the status markers (error / edited / complete) to the tab links, and remembers the active tab per
    // page, so a reload or a round trip to the server (validation errors) returns to it.
    private buildTabs($form: JQuery, $tabs: JQuery, sections: Section[]): { refresh: () => void, rebase: () => void } {
        const links = $tabs.find("a[data-toggle=tab]").toArray();
        const key = "sm-form-tab:" + window.location.pathname + "#" + ($form.attr("id") || "form");
        const baselines: string[] = [];
        const markers = links.map((l) => {
            const $marker = $("<span>").addClass("form-index-mark");

            $(l).prepend($marker);

            return $marker;
        });

        const paneOf = (l: HTMLElement) => $($(l).attr("href") || "");

        const rebase = () => links.forEach((l, i) => baselines[i] = RaceForm.signature(paneOf(l)));
        const refresh = () => {
            links.forEach((l, i) => {
                const $pane = paneOf(l);
                let status = "";
                let text = "";

                if ($pane.find(".has-error, [aria-invalid=true]").length) {
                    status = "error";
                    text = t("race_form.status.has_errors");
                } else if (RaceForm.signature($pane) !== baselines[i]) {
                    status = "edited";
                    text = t("race_form.edited");
                }

                markers[i].attr("data-status", status).empty();

                if (text) {
                    markers[i].append($("<span>").addClass("sr-only").text(text + ": "));
                }
            });
        };

        rebase();

        try {
            const stored = sessionStorage.getItem(key);
            const $link = stored ? $tabs.find("a[data-toggle=tab][href=\"" + stored.replace(/"/g, "") + "\"]") : $();

            if ($link.length) {
                $link.first().tab("show");
            }
        } catch (e) {
            // storage can be unavailable: the first tab is shown
        }

        $tabs.on("shown.bs.tab", (e: any) => {
            try {
                sessionStorage.setItem(key, $(e.target).attr("href") || "");
            } catch (err) {
                // see above
            }

            // widgets that measure their layout (switches, editors) get a chance to update
            $(window).trigger("resize");
        });

        $form.on("input change", () => setTimeout(refresh, 150));
        refresh();

        return {refresh, rebase};
    }

    private buildIndex($form: JQuery, sections: Section[]): () => void {
        if (sections.length < 3) {
            return () => undefined;
        }

        const $index = $("<nav>").addClass("form-index").attr("aria-label", t("race_form.sections"));
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
                    text = t("race_form.status.has_errors");
                } else if (RaceForm.signature(s.$card) !== s.baseline) {
                    status = "edited";
                    text = t("race_form.edited");
                } else if ($required.length && $required.toArray().every((el: any) => el.checkValidity())) {
                    status = "complete";
                    text = t("race_form.status.complete");
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

            return text || el.getAttribute("name") || t("race_form.field_fallback");
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
                .append($("<strong>").text(t("race_form.fix_fields")));
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

            // the first invalid field decides which tab is shown (the browser focuses the first invalid field)
            if (!batchOpen) {
                RaceForm.showTabOf(el);
            }

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

                RaceForm.showTabOf(el);
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

        let tabShown = false;

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

            if (!tabShown) {
                tabShown = true;
                RaceForm.showTabOf(el);
            }

            $a.on("click", (ev) => {
                ev.preventDefault();

                if (s) {
                    RaceForm.setOpen(s, true);
                }

                RaceForm.showTabOf(el);
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
        const $text = $("<span>").addClass("save-bar-text").text(t("race_form.no_changes"));
        $bar.append($text);

        let dirty = false;
        let submitted = false;

        // Discard throws the edits away by loading the page again; the shared dialog asks first when there are edits
        const $discard = $("<button>").attr("type", "button").addClass("btn btn-outline race-discard").text(t("race_form.discard_changes"));
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

            $("#delete-confirm-title").text(t("race_form.discard_title"));
            $("#delete-confirm-detail").text(t("race_form.discard_detail"));
            $("#delete-confirm-ok").text(t("race_form.discard")).one("click.discard", reload);
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
            $text.text(t("race_form.unsaved_changes")).addClass("is-dirty");
        });

        $form.on("input change click keyup", () => setTimeout(sync, 0));
        $form.on("submit", () => submitted = true);
    }
}
