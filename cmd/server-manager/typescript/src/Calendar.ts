import { Calendar } from '@fullcalendar/core';
import timeGridPlugin from '@fullcalendar/timegrid';
import listPlugin from '@fullcalendar/list';
import bootstrapPlugin from '@fullcalendar/bootstrap';

// serverLabel returns the HTML which tells on which server an event is scheduled, if that is worth showing.
function serverLabel(props: any): string {
    if (!props.scheduledServerName && !props.scheduledServerID) {
        return '';
    }

    let name = $('<span>').text(props.scheduledServerName || 'another server').html();

    return '<div class="calendar-small">On <span class="scheduled-server-id" data-server-id="' + $('<span>').text(props.scheduledServerID).html() + '">' + name + '</span></div>';
}

document.addEventListener('DOMContentLoaded', function() {
    let calendarEl = document.getElementById('calendar');

    if (!calendarEl) {
        return
    }

    let calendar = new Calendar(calendarEl, {
        plugins: [ timeGridPlugin, listPlugin, bootstrapPlugin ],
        defaultView: 'timeGridThreeDay',
        events: '/calendar.json' + window.location.search.replace(/[?&]server=[^&]*/g, '').replace(/^&/, '?'),
        themeSystem: 'bootstrap',

        header: {
            center: 'timeGridWeek,timeGridThreeDay,listWeek' // buttons for switching between views
        },
        views: {
            timeGridThreeDay: {
                type: 'timeGrid',
                duration: { days: 3 },
                buttonText: '3 day'
            }
        },

        eventRender: function(info) {
            let $title = $(info.el).find('.fc-title');
            let $time = $(info.el).find('.fc-time');


            if (info.event.extendedProps.signUpURL) {
                $time.append('<a class="calendar-signup-link" href="'+info.event.extendedProps.signUpURL+'">Event Sign Up</a>')
            }

            $title.append('<div class="hr-line-solid-no-margin"></div><span class="calendar-small">'+info.event.extendedProps.description+'</span></div>');

            $title.append(serverLabel(info.event.extendedProps));

            let $listTitle = $(info.el).find('.fc-list-item-title');

            if (info.event.extendedProps.signUpURL) {
                $listTitle.append('</div><a class="calendar-signup-link" href="'+info.event.extendedProps.signUpURL+'">Event Sign Up</a>')
            }

            $listTitle.append('<div class="ml-2"></div><span class="calendar-small">'+info.event.extendedProps.description+'</span></div>');

            $listTitle.append(serverLabel(info.event.extendedProps));
        },

        nowIndicator: true,
        allDaySlot: false,
        timeGridEventMinHeight: 100,
        height: 800,

        contentHeight: 1000,
    });

    calendar.render();
});
