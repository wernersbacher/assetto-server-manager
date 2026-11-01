import "jquery";
import "bootstrap";
import "bootstrap-switch";
import "summernote/dist/summernote-bs4";
import "multiselect";
import "moment";
import "moment-timezone";
import "./i18n";
import {EntryPoint} from "./javascript/manager";
import "./Font";
import "./Calendar";

import {RaceControl} from "./RaceControl";
import {CarDetail} from "./CarDetail";
import {TrackDetail} from "./TrackDetail";
import {CarSearch} from "./CarSearch";
import {CarList} from "./CarList";
import {RaceWeekend} from "./RaceWeekend";
import {ChangelogPopup} from "./ChangelogPopup";
import {Messages} from "./Messages";
import {Championship} from "./Championship";
import {Results} from "./Results";
import {RaceList} from "./RaceList";
import {SpectatorCar} from "./SpectatorCar";
import {Form} from "./Form";
import {Shell} from "./Shell";
import {DeleteConfirm} from "./DeleteConfirm";
import {ToggleOverride} from "./ToggleOverride";
import {AdminForm} from "./AdminForm";
import {RaceForm} from "./RaceForm";

$(() => {
    new Shell();
    new DeleteConfirm();
    new ToggleOverride();
    new AdminForm();
    new Form();
    EntryPoint();
    new RaceForm();
    new RaceControl();
    new CarDetail();
    new TrackDetail();
    new CarList();
    new RaceWeekend.View();
    new RaceWeekend.EditSession();
    new ChangelogPopup();
    Messages.initSummerNote();
    new Championship.View();
    new Results();
    new RaceList();
    new SpectatorCar();

    $(".race-setup").each(function (index, elem) {
        new CarSearch($(elem));
    });

    // the blocks of a championship class (one per tab) share a key and are searched as one
    const classKeys: string[] = [];

    $("[data-class-key]").each(function (index, elem) {
        const key = $(elem).attr("data-class-key") as string;

        if (classKeys.indexOf(key) < 0 && !$(elem).closest("[data-class-template]").length) {
            classKeys.push(key);
            new CarSearch($("[data-class-key='" + key + "']"));
        }
    });
});

declare global {
    interface JQuery {
        multiSelect: any;
        select2: any;
    }
}
