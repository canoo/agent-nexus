use tauri::{
    image::Image,
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};

const COLLECTION_DISABLED_LABEL: &str = "Collection: Disabled";
const COLLECTION_DISABLED_TOOLTIP: &str = "NEXUS Companion — collection disabled";

fn collection_disabled_icon() -> Image<'static> {
    const SIDE: usize = 32;
    let mut rgba = vec![0; SIDE * SIDE * 4];

    for y in 0..SIDE {
        for x in 0..SIDE {
            let index = (y * SIDE + x) * 4;
            let is_nexus_mark = (x == 8 && (7..25).contains(&y))
                || (x == 23 && (7..25).contains(&y))
                || (x == y && (7..25).contains(&x))
                || (x + y == 31 && (7..25).contains(&x));

            if is_nexus_mark {
                rgba[index..index + 4].copy_from_slice(&[92, 106, 196, 255]);
            }
        }
    }

    Image::new_owned(rgba, SIDE as u32, SIDE as u32)
}

fn show_dashboard(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn main() {
    tauri::Builder::default()
        .setup(|app| {
            let open_dashboard =
                MenuItem::with_id(app, "open-dashboard", "Open Dashboard", true, None::<&str>)?;
            let collection_disabled = MenuItem::with_id(
                app,
                "collection-disabled",
                COLLECTION_DISABLED_LABEL,
                false,
                None::<&str>,
            )?;
            let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&open_dashboard, &collection_disabled, &quit])?;

            TrayIconBuilder::with_id("companion-status")
                .icon(collection_disabled_icon())
                .tooltip(COLLECTION_DISABLED_TOOLTIP)
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "open-dashboard" => show_dashboard(app),
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("NEXUS Companion desktop shell failed to start");
}
