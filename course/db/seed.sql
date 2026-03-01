INSERT INTO "Course" ("course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite") VALUES
(1, 'Mathematics',      3, ARRAY['1', '2'], 'Monday',    '09:00:00', '12:00:00', 30, 'open', ARRAY['3'],   NULL),
(2, 'Physics',          3, ARRAY['1', '3'], 'Tuesday',   '13:00:00', '16:00:00', 30, 'open', ARRAY['1'],   ARRAY['Mathematics']),
(3, 'Computer Science', 3, ARRAY['1'],      'Wednesday', '09:00:00', '13:00:00', 80, 'open', ARRAY['2'],   NULL);
