-- ===========================
-- Insert data into Student
-- ===========================
INSERT INTO "Student" ("student_id", "first_name", "last_name", "email", "birthdate", "gender", "year_level", "graded_subject") VALUES
(1, 'Somchai',  'Rakdee',    'somchai.r@example.com',  '2003-05-12', 'Male',   1, ARRAY['Mathematics']),
(2, 'Nattaya',  'Srisuwan',  'nattaya.s@example.com',  '2002-08-24', 'Female', 2, ARRAY['Mathematics', 'Physics']),
(3, 'Wichai',   'Pornpan',   'wichai.p@example.com',   '2003-01-30', 'Male',   1, ARRAY['Computer Science']),
(4, 'Siriporn', 'Kaewmala',  'siriporn.k@example.com', '2001-11-05', 'Female', 3, ARRAY['Mathematics', 'Physics', 'Computer Science']),
(5, 'Anuwat',   'Thongsuk',  'anuwat.t@example.com',   '2002-03-17', 'Male',   2, ARRAY['Computer Science']);


-- ===========================
-- Insert data into Course
-- ===========================
INSERT INTO "Course" ("course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "prerequisite") VALUES
(1, 'Mathematics',      3, ARRAY['1', '2'],       'Monday',    '09:00:00', '12:00:00', 30, 'open', NULL),
(2, 'Physics',          3, ARRAY['1', '3'],       'Tuesday',   '13:00:00', '16:00:00', 30, 'open', 'Mathematics'),
(3, 'Computer Science', 3, ARRAY['1'],       'Wednesday', '09:00:00', '13:00:00', 80, 'open', NULL);


-- ===========================
-- Insert data into student_course
-- ===========================
INSERT INTO "student_course" ("student_id", "course_id") VALUES
(1, 1),           -- Somchai    -> Mathematics
(2, 1),           -- Nattaya    -> Mathematics
(2, 2),           -- Nattaya    -> Physics
(3, 3),           -- Wichai     -> Computer Science
(4, 1),           -- Siriporn   -> Mathematics
(4, 2),           -- Siriporn   -> Physics
(4, 3),           -- Siriporn   -> Computer Science
(5, 3);           -- Anuwat     -> Computer Science
