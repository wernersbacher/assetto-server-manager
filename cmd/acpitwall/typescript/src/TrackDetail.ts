import ClickEvent = JQuery.ClickEvent;
import {SummernoteWrapper} from "./forms/SummernoteWrapper";
import {t} from "./i18n";

export class TrackDetail {
    public constructor() {
        if (!$(".track-details").length) {
            return;
        }

        $(".track-image").on("click", TrackDetail.onTrackLayoutClick);

        TrackDetail.fixLayoutImageHeights();
        TrackDetail.initSummerNote();
        $(window).on("resize", TrackDetail.fixLayoutImageHeights);
    }

    private static onTrackLayoutClick(e: ClickEvent) {
        const $currentTarget = $(e.currentTarget);

        $("#hero-skin").attr({
            "src": $currentTarget.attr("src"),
            "alt": $currentTarget.attr("alt"),
        });

        $("select[name='skin-delete']").val($currentTarget.data("layout"));
    }

    private static fixLayoutImageHeights() {
        $(".track-layouts").height($("#hero-skin").height()!);
    }

    private static initSummerNote() {
        let $summerNote = $("#summernote");
        let $trackNotes = $("#TrackNotes");

        let html = "";

        if ($trackNotes.length > 0) {
            html = $trackNotes.html();
        }

        let wrapper = new SummernoteWrapper($summerNote, {
            placeholder: t("content.track_notes_placeholder"),
            tabsize: 2,
            height: 200,
        }, html);

        wrapper.render();
    }
}
